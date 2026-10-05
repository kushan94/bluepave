package jobs

import (
	"errors"
	"strings"
	"testing"
)

func TestProcess(t *testing.T) {
	tests := []struct {
		kind  Kind
		input string
		want  string
	}{
		{KindWordCount, "the quick brown fox", "4 words"},
		{KindWordCount, "  single  ", "1 word"},
		{KindUppercase, "Azure ❤ k8s", "AZURE ❤ K8S"},
		{KindReverse, "héllo", "olléh"},
		{KindReverse, "a", "a"},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind)+"/"+tt.input, func(t *testing.T) {
			got, err := Process(tt.kind, tt.input)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if got != tt.want {
				t.Errorf("Process(%q, %q) = %q, want %q", tt.kind, tt.input, got, tt.want)
			}
		})
	}
}

func TestProcessUnknownKind(t *testing.T) {
	if _, err := Process("delete-everything", "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name  string
		kind  Kind
		input string
		ok    bool
	}{
		{"valid", KindUppercase, "hello", true},
		{"unknown kind", "rm", "hello", false},
		{"empty", KindReverse, "   ", false},
		{"at limit", KindReverse, strings.Repeat("é", MaxInputLength), true},
		{"over limit", KindReverse, strings.Repeat("a", MaxInputLength+1), false},
		{"invalid utf8", KindReverse, "\xff\xfe", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.kind, tt.input)
			if tt.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestStatusFinished(t *testing.T) {
	for status, want := range map[Status]bool{
		StatusQueued: false, StatusRunning: false, StatusDone: true, StatusFailed: true,
	} {
		if got := status.Finished(); got != want {
			t.Errorf("%s.Finished() = %v, want %v", status, got, want)
		}
	}
}
