package manager

import (
	"crypto/rand"
	"fmt"
)

// The pools keep generated names short and easy to say: an -ing verb, an
// adjective, and a noun.
var generatedNamePools = [3][]string{
	{"balancing", "blazing", "braving", "climbing", "dancing", "drifting", "driving", "flying", "gliding", "growing", "hiking", "jogging", "leaping", "marching", "rolling", "running", "sailing", "soaring", "spinning", "trekking", "wandering", "whistling", "winning", "zooming"},
	{"amber", "bright", "calm", "clever", "cosmic", "crisp", "daring", "eager", "gentle", "golden", "grand", "happy", "jolly", "lively", "mellow", "nimble", "playful", "quiet", "rapid", "shiny", "silent", "steady", "swift", "vivid"},
	{"badger", "falcon", "fox", "koala", "lion", "moon", "otter", "panda", "pebble", "raven", "river", "robin", "spark", "star", "stone", "sunset", "tiger", "trail", "unicorn", "valley", "willow", "wind", "wolf", "yarrow"},
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
