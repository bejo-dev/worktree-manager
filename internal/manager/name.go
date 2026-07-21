package manager

import (
	"crypto/rand"
	"fmt"
)

// These deliberately small pools keep generated names short and easy to say.
var generatedNamePools = [3][]string{
	{"amber", "bright", "calm", "clever", "cosmic", "gentle", "swift", "quiet"},
	{"badger", "falcon", "fox", "koala", "otter", "panda", "raven", "tiger"},
	{"bloom", "drift", "glow", "leap", "roam", "spark", "stride", "wave"},
}

// randomWorktreeName returns one word from each of the three name pools.
func randomWorktreeName() (string, error) {
	words := [3]string{}
	for i, pool := range generatedNamePools {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("random name: %w", err)
		}
		words[i] = pool[int(b[0])%len(pool)]
	}
	return fmt.Sprintf("%s-%s-%s", words[0], words[1], words[2]), nil
}
