package jlpt_test

import (
	"context"
	"testing"

	"github.com/rikiisworking/jkrt/internal/jlpt"
)

func TestBuildArgs(t *testing.T) {
	h := jlpt.NewHeadless(jlpt.HeadlessConfig{Model: "composer-2.5"})
	args := h.BuildArgs("hello")
	joined := stringsJoin(args)
	for _, want := range []string{"-p", "hello", "-m", "composer-2.5", "--output-format", "json", "--max-turns", "1"} {
		if !containsArg(args, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
	_ = joined
}

func TestParseClassifyOutputDirect(t *testing.T) {
	lv, err := jlpt.ParseClassifyOutput(`{"level":"n1"}`)
	if err != nil || lv != jlpt.N1 {
		t.Fatalf("%v %v", lv, err)
	}
}

func TestParseClassifyOutputWrapped(t *testing.T) {
	lv, err := jlpt.ParseClassifyOutput(`{"result":"{\"level\":\"n2\"}"}`)
	if err != nil || lv != jlpt.N2 {
		t.Fatalf("%v %v", lv, err)
	}
}

func TestParseClassifyOutputProse(t *testing.T) {
	lv, err := jlpt.ParseClassifyOutput("Sure: {\"level\":\"n3\"} done")
	if err != nil || lv != jlpt.N3 {
		t.Fatalf("%v %v", lv, err)
	}
}

func TestHeadlessRunner(t *testing.T) {
	h := jlpt.NewHeadless(jlpt.HeadlessConfig{
		Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte(`{"level":"n1"}`), nil
		},
	})
	lv, err := h.Classify(context.Background(), "珍語", "ちんご")
	if err != nil || lv != jlpt.N1 {
		t.Fatalf("%v %v", lv, err)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func stringsJoin(args []string) string {
	s := ""
	for _, a := range args {
		s += a + " "
	}
	return s
}
