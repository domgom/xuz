package cmdx

import "testing"

func TestSubstitute(t *testing.T) {
	vars := map[string]string{"MODEL": "/m/a.gguf", "CONTEXT": "65536"}
	got := Substitute(`exec llama-server -m "$MODEL" -c "$CONTEXT" -t 4`, vars)
	want := `exec llama-server -m "/m/a.gguf" -c "65536" -t 4`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	got = Substitute(`echo ${MODEL} ${MISSING}`, vars)
	want = `echo /m/a.gguf ${MISSING}`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
