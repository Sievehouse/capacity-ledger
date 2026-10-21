// Command capledger is the Capacity Ledger command-line tool: the in-cluster
// agent, the SLA engine and the evidence-log utilities in one static binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Sievehouse/capacity-ledger/internal/agent"
	"github.com/Sievehouse/capacity-ledger/internal/contract"
	"github.com/Sievehouse/capacity-ledger/internal/events"
	"github.com/Sievehouse/capacity-ledger/internal/evidence"
	"github.com/Sievehouse/capacity-ledger/internal/sla"
	"github.com/Sievehouse/capacity-ledger/internal/xid"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

const usageText = `capledger: contract-grade acceptance and SLA evidence for GPU clusters

Usage:
  capledger <command> [flags]

Commands:
  agent             watch kernel logs for NVIDIA XID/SXID faults and record signed evidence
  sla compute       compute a sealed SLA statement from a contract and events
  sla verify        check a statement's seal, or replay it from its inputs
  event emit        append one signed availability event (for integrations and operators)
  events export     extract events from one or more evidence logs as JSON Lines
  evidence verify   verify hash chains and signatures of evidence logs
  evidence append   append a signed entry (for example a statement) to a log
  xid parse         parse kernel log lines and show how the policy classifies them
  xid policy        print the built-in starter XID policy
  keygen            create an Ed25519 signing key pair
  version           print the version

Run "capledger <command> -h" for the flags of a command.
`

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	var err error
	switch {
	case args[0] == "version" || args[0] == "--version":
		fmt.Fprintf(stdout, "capledger %s (sla engine %s)\n", version, sla.EngineVersion)
		return 0
	case args[0] == "help" || args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	case args[0] == "keygen":
		err = cmdKeygen(args[1:], stdout)
	case args[0] == "agent":
		err = cmdAgent(args[1:], stdin, stderr)
	case args[0] == "sla" && sub == "compute":
		err = cmdSLACompute(args[2:], stdout)
	case args[0] == "sla" && sub == "verify":
		err = cmdSLAVerify(args[2:], stdout)
	case args[0] == "event" && sub == "emit":
		err = cmdEventEmit(args[2:], stdout)
	case args[0] == "events" && sub == "export":
		err = cmdEventsExport(args[2:], stdout, stderr)
	case args[0] == "evidence" && sub == "verify":
		err = cmdEvidenceVerify(args[2:], stdout)
	case args[0] == "evidence" && sub == "append":
		err = cmdEvidenceAppend(args[2:], stdin, stdout)
	case args[0] == "xid" && sub == "parse":
		err = cmdXIDParse(args[2:], stdin, stdout)
	case args[0] == "xid" && sub == "policy":
		_, err = stdout.Write(xid.DefaultPolicyJSON())
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", strings.Join(args[:min(2, len(args))], " "), usageText)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var ce codedError
		if errors.As(err, &ce) {
			fmt.Fprintln(stderr, "capledger:", ce.msg)
			return ce.code
		}
		fmt.Fprintln(stderr, "capledger:", err)
		return 1
	}
	return 0
}

// codedError carries a specific exit code (1 = verification failed).
type codedError struct {
	code int
	msg  string
}

func (e codedError) Error() string { return e.msg }

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func required(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.TrimSpace(pairs[i+1]) == "" {
			return fmt.Errorf("--%s is required", pairs[i])
		}
	}
	return nil
}

func cmdKeygen(args []string, stdout io.Writer) error {
	fs := newFlags("keygen")
	out := fs.String("out", "keys", "directory for the key pair")
	if err := fs.Parse(args); err != nil {
		return err
	}
	priv, pub, id, err := evidence.GenerateKeyPair(*out)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "key id      %s\nprivate key %s (keep secret)\npublic key  %s (share with counterparties)\n", id, priv, pub)
	return nil
}

func parsePeriod(period, start, end string) (sla.Period, error) {
	if period != "" {
		t, err := time.Parse("2006-01", period)
		if err != nil {
			return sla.Period{}, fmt.Errorf("--period must look like 2026-10: %w", err)
		}
		return sla.Period{Start: t.Unix(), End: t.AddDate(0, 1, 0).Unix()}, nil
	}
	if start == "" || end == "" {
		return sla.Period{}, errors.New("give --period YYYY-MM or both --start and --end (RFC 3339)")
	}
	s, err := events.ParseTime(start)
	if err != nil {
		return sla.Period{}, fmt.Errorf("--start: %w", err)
	}
	e, err := events.ParseTime(end)
	if err != nil {
		return sla.Period{}, fmt.Errorf("--end: %w", err)
	}
	p := sla.Period{Start: s, End: e}
	return p, p.Validate()
}

func loadEvents(paths []string) ([]events.Event, error) {
	var all []events.Event
	for _, p := range paths {
		evs, err := events.ReadFile(p)
		if err != nil {
			return nil, err
		}
		all = append(all, evs...)
	}
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.ID] {
			return nil, fmt.Errorf("event id %q appears in more than one file", e.ID)
		}
		seen[e.ID] = true
	}
	events.Sort(all)
	return all, nil
}

func cmdSLACompute(args []string, stdout io.Writer) error {
	fs := newFlags("sla compute")
	contractPath := fs.String("contract", "", "contract spec (JSON)")
	var eventPaths stringList
	fs.Var(&eventPaths, "events", "events file (JSON Lines); repeat for several files")
	period := fs.String("period", "", "calendar month in UTC, e.g. 2026-10")
	start := fs.String("start", "", "period start (RFC 3339), instead of --period")
	end := fs.String("end", "", "period end, exclusive (RFC 3339)")
	out := fs.String("out", "", "write the statement JSON to this file")
	format := fs.String("format", "json", "stdout format: json or text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("contract", *contractPath); err != nil {
		return err
	}
	if len(eventPaths) == 0 {
		return errors.New("--events is required")
	}
	p, err := parsePeriod(*period, *start, *end)
	if err != nil {
		return err
	}
	spec, err := contract.Load(*contractPath)
	if err != nil {
		return err
	}
	evs, err := loadEvents(eventPaths)
	if err != nil {
		return err
	}
	st, err := sla.Compute(spec, evs, p)
	if err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	pretty = append(pretty, '\n')
	if *out != "" {
		if err := os.WriteFile(*out, pretty, 0o644); err != nil {
			return err
		}
	}
	switch *format {
	case "text":
		_, err = io.WriteString(stdout, st.Text())
	case "json":
		if *out == "" {
			_, err = stdout.Write(pretty)
		} else {
			fmt.Fprintf(stdout, "wrote %s (statementSha256 %s)\n", *out, st.StatementSHA256)
		}
	default:
		err = fmt.Errorf("unknown --format %q", *format)
	}
	return err
}

func cmdSLAVerify(args []string, stdout io.Writer) error {
	fs := newFlags("sla verify")
	stmtPath := fs.String("statement", "", "statement JSON to check")
	contractPath := fs.String("contract", "", "contract spec; with --events, replay the computation")
	var eventPaths stringList
	fs.Var(&eventPaths, "events", "events file; repeat for several files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("statement", *stmtPath); err != nil {
		return err
	}
	st, err := sla.ReadStatement(*stmtPath)
	if err != nil {
		return err
	}
	if *contractPath == "" && len(eventPaths) == 0 {
		if err := st.CheckSeal(); err != nil {
			return codedError{1, err.Error()}
		}
		fmt.Fprintf(stdout, "seal OK: %s\n(pass --contract and --events to replay the computation as well)\n", st.StatementSHA256)
		return nil
	}
	if *contractPath == "" || len(eventPaths) == 0 {
		return errors.New("replay needs both --contract and --events")
	}
	spec, err := contract.Load(*contractPath)
	if err != nil {
		return err
	}
	evs, err := loadEvents(eventPaths)
	if err != nil {
		return err
	}
	if err := sla.Replay(spec, evs, st); err != nil {
		return codedError{1, "replay failed: " + err.Error()}
	}
	fmt.Fprintf(stdout, "replay OK: same inputs, same statement %s\n", st.StatementSHA256)
	return nil
}

func openWriter(logPath, keyPath string) (*evidence.Writer, error) {
	if err := required("log", logPath, "key", keyPath); err != nil {
		return nil, err
	}
	priv, err := evidence.LoadPrivateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return evidence.OpenWriter(logPath, priv)
}

func cmdEventEmit(args []string, stdout io.Writer) error {
	fs := newFlags("event emit")
	logPath := fs.String("log", "", "evidence log to append to")
	keyPath := fs.String("key", "", "private signing key (PEM)")
	node := fs.String("node", "", "node name as listed in the contract")
	typ := fs.String("type", "", "down | up | degraded_start | degraded_end | maintenance_start | maintenance_end")
	cause := fs.String("cause", "", "cause, e.g. xid:79, network:rail-3, customer:oom, repaired")
	ts := fs.String("ts", "", "event time (RFC 3339); default now")
	notice := fs.String("notice-ts", "", "for maintenance_start: when the window was announced (RFC 3339)")
	source := fs.String("source", "manual", "who or what produced the event")
	note := fs.String("note", "", "free-text note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("node", *node, "type", *typ); err != nil {
		return err
	}
	w, err := openWriter(*logPath, *keyPath)
	if err != nil {
		return err
	}
	if *ts == "" {
		*ts = time.Now().UTC().Format(time.RFC3339)
	}
	// Seq is the evidence sequence number, so events written in the same
	// second keep their recorded order (ids alone would sort "-10" before "-9").
	ev := events.Event{
		ID: fmt.Sprintf("%s-%d", w.KeyID(), w.NextSeq()), Seq: w.NextSeq(), TS: *ts, Node: *node, Type: events.Type(*typ),
		Cause: *cause, NoticeTS: *notice, Source: *source, Note: *note,
	}
	if err := ev.Normalize(); err != nil {
		return err
	}
	entry, err := w.Append("event", ev)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "appended event %s (seq %d, hash %s)\n", ev.ID, entry.Seq, entry.Hash)
	return nil
}

func verifyLogs(paths, pubPaths []string) ([]evidence.Report, bool, error) {
	keys, err := evidence.LoadPublicKeys(pubPaths)
	if err != nil {
		return nil, false, err
	}
	ok := true
	var reps []evidence.Report
	for _, p := range paths {
		rep := evidence.VerifyFile(p, keys)
		ok = ok && rep.OK
		reps = append(reps, rep)
	}
	return reps, ok, nil
}

func cmdEventsExport(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("events export")
	var logs, pubs stringList
	fs.Var(&logs, "log", "evidence log; repeat for several logs")
	fs.Var(&pubs, "pub", "trusted public key; repeat as needed (required unless --no-verify)")
	noVerify := fs.Bool("no-verify", false, "skip signature verification (not for contract use)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(logs) == 0 {
		return errors.New("--log is required")
	}
	if !*noVerify {
		if len(pubs) == 0 {
			return errors.New("--pub is required to verify logs before export (or pass --no-verify)")
		}
		reps, ok, err := verifyLogs(logs, pubs)
		if err != nil {
			return err
		}
		if !ok {
			for _, r := range reps {
				for _, p := range r.Problems {
					fmt.Fprintf(stderr, "%s: %s\n", r.Path, p)
				}
			}
			return codedError{1, "evidence verification failed; nothing exported"}
		}
	}
	var all []events.Event
	for _, p := range logs {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		entries, err := evidence.ReadEntries(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		for _, e := range entries {
			if e.Kind != "event" {
				continue
			}
			var ev events.Event
			if err := e.DecodePayload(&ev); err != nil {
				return fmt.Errorf("%s seq %d: %w", p, e.Seq, err)
			}
			if ev.Evidence == "" {
				ev.Evidence = e.Hash
			}
			if err := ev.Normalize(); err != nil {
				return fmt.Errorf("%s seq %d: %w", p, e.Seq, err)
			}
			all = append(all, ev)
		}
	}
	events.Sort(all)
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	for _, ev := range all {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return nil
}

func cmdEvidenceVerify(args []string, stdout io.Writer) error {
	fs := newFlags("evidence verify")
	var logs, pubs stringList
	fs.Var(&logs, "log", "evidence log; repeat for several logs")
	fs.Var(&pubs, "pub", "trusted public key (PEM); repeat as needed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(logs) == 0 || len(pubs) == 0 {
		return errors.New("--log and --pub are required")
	}
	reps, ok, err := verifyLogs(logs, pubs)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(reps, "", "  ")
	fmt.Fprintln(stdout, string(b))
	if !ok {
		return codedError{1, "evidence verification failed"}
	}
	return nil
}

func cmdEvidenceAppend(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := newFlags("evidence append")
	logPath := fs.String("log", "", "evidence log")
	keyPath := fs.String("key", "", "private signing key (PEM)")
	kind := fs.String("kind", "", "entry kind, e.g. statement, acceptance-report, test-result")
	payloadPath := fs.String("payload", "", "JSON file to record, or - for stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("kind", *kind, "payload", *payloadPath); err != nil {
		return err
	}
	w, err := openWriter(*logPath, *keyPath)
	if err != nil {
		return err
	}
	var raw []byte
	if *payloadPath == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(*payloadPath)
	}
	if err != nil {
		return err
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("payload is not valid JSON: %w", err)
	}
	entry, err := w.Append(*kind, json.RawMessage(raw))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "appended %s entry (seq %d, hash %s)\n", *kind, entry.Seq, entry.Hash)
	return nil
}

func cmdXIDParse(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := newFlags("xid parse")
	policyPath := fs.String("policy", "", "XID policy JSON (default: built-in starter policy)")
	file := fs.String("file", "", "kernel log file (default: stdin)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	policy, err := loadPolicy(*policyPath)
	if err != nil {
		return err
	}
	in := stdin
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		o, ok := xid.ParseLine(line)
		if !ok {
			continue
		}
		r := policy.Classify(o)
		if err := enc.Encode(map[string]any{"observation": o, "action": r.Action, "summary": r.Summary}); err != nil {
			return err
		}
	}
	return nil
}

func loadPolicy(path string) (xid.Policy, error) {
	if path == "" {
		return xid.DefaultPolicy()
	}
	return xid.LoadPolicy(path)
}

func cmdAgent(args []string, stdin io.Reader, stderr io.Writer) error {
	fs := newFlags("agent")
	hostname, _ := os.Hostname()
	node := fs.String("node", hostname, "node name as listed in the contract")
	source := fs.String("source", "kmsg", "kmsg (/dev/kmsg), a log file path, or - for stdin")
	once := fs.Bool("once", false, "read the source to the end and exit (replay mode)")
	fromStart := fs.Bool("from-start", false, "when following, start at the beginning instead of the end")
	logPath := fs.String("log", "", "evidence log to append to")
	keyPath := fs.String("key", "", "private signing key (PEM)")
	policyPath := fs.String("policy", "", "XID policy JSON (default: built-in starter policy)")
	suppress := fs.Duration("suppress", 10*time.Minute, "minimum gap between down events for this node")
	expected := fs.Int("expected-gpus", 0, "if > 0, poll nvidia-smi and record a down event when fewer GPUs are visible")
	smiEvery := fs.Duration("smi-interval", time.Minute, "nvidia-smi polling interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := openWriter(*logPath, *keyPath)
	if err != nil {
		return err
	}
	policy, err := loadPolicy(*policyPath)
	if err != nil {
		return err
	}
	a, err := agent.New(agent.Config{Node: *node, Policy: policy, Writer: w, Suppress: *suppress, Log: stderr})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(stderr, "capledger agent %s: node %s, key %s, policy %s, source %s\n", version, *node, w.KeyID(), policy.Version, *source)
	if *expected > 0 && !*once {
		go pollGPUs(ctx, a, *expected, *smiEvery, stderr)
	}

	switch {
	case *source == "-":
		err = a.Run(ctx, stdin)
	case *once:
		f, ferr := os.Open(*source)
		if ferr != nil {
			return ferr
		}
		err = a.Run(ctx, f)
		f.Close()
	case *source == "kmsg":
		err = agent.FollowKmsg(ctx, *fromStart, a.HandleLine)
	default:
		err = agent.FollowFile(ctx, *source, *fromStart, 500*time.Millisecond, a.HandleLine)
	}
	st := a.Stats()
	fmt.Fprintf(stderr, "processed %d lines: %d observations, %d events, %d suppressed\n", st.Lines, st.Observations, st.Events, st.Suppressed)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// pollGPUs is a coarse liveness check: a GPU that falls off the bus usually
// disappears from nvidia-smi. Errors running nvidia-smi are logged, not
// treated as faults, because they can come from the tool rather than the GPU.
func pollGPUs(ctx context.Context, a *agent.Agent, expected int, every time.Duration, log io.Writer) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, err := exec.CommandContext(cctx, "nvidia-smi", "--query-gpu=index,pci.bus_id", "--format=csv,noheader").Output()
		cancel()
		if err != nil {
			fmt.Fprintf(log, "nvidia-smi failed: %v\n", err)
		} else if err := a.HandleGPUCount(agent.CountGPUs(string(out)), expected); err != nil {
			fmt.Fprintf(log, "recording GPU count failed: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
