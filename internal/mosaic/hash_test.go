package mosaic

import (
	"strings"
	"testing"
)

func TestName_DeterministicAndInputSensitive(t *testing.T) {
	a := Name([]string{"art/a.jpg", "art/b.jpg"})
	if a != Name([]string{"art/a.jpg", "art/b.jpg"}) {
		t.Error("Name must be deterministic for identical input")
	}
	if !strings.HasSuffix(a, ".jpg") {
		t.Errorf("Name = %q, want .jpg suffix", a)
	}
	if a == Name([]string{"art/b.jpg", "art/a.jpg"}) {
		t.Error("order must change the hash")
	}
	if a == Name([]string{"art/a.jpg"}) {
		t.Error("different inputs must differ")
	}
}
