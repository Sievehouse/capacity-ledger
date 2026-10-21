package evidence

import "crypto/sha256"

// MerkleRoot returns the RFC 6962 Merkle tree hash over the given leaves.
// Leaf and interior hashes are domain-separated (0x00 and 0x01 prefixes), so
// a leaf can never be confused with an interior node.
func MerkleRoot(leaves [][]byte) []byte {
	n := len(leaves)
	if n == 0 {
		sum := sha256.Sum256(nil)
		return sum[:]
	}
	if n == 1 {
		return leafHash(leaves[0])
	}
	k := 1
	for k*2 < n {
		k *= 2
	}
	return nodeHash(MerkleRoot(leaves[:k]), MerkleRoot(leaves[k:]))
}

func leafHash(b []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(b)
	return h.Sum(nil)
}

func nodeHash(l, r []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x01})
	h.Write(l)
	h.Write(r)
	return h.Sum(nil)
}
