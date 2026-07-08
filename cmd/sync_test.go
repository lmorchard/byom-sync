package cmd

import (
	"reflect"
	"testing"
)

func TestSelectTargets(t *testing.T) {
	config := []string{"cfg1", "cfg2"}

	// Positional args override the config list entirely.
	if got := selectTargets([]string{"argA", "argB"}, config); !reflect.DeepEqual(got, []string{"argA", "argB"}) {
		t.Errorf("args should override config: got %v", got)
	}

	// No args → use config list.
	if got := selectTargets(nil, config); !reflect.DeepEqual(got, config) {
		t.Errorf("empty args should use config: got %v", got)
	}

	// Both empty → empty.
	if got := selectTargets(nil, nil); len(got) != 0 {
		t.Errorf("both empty should be empty: got %v", got)
	}
}
