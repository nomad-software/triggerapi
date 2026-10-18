package ui

import (
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/mcp"
)

func TestSamplingDialog(t *testing.T) {
	s := &mcp.Sampling{
		SystemPrompt: "Be brief.",
		Messages:     []mcp.SamplingMessage{{Role: "user", Text: "Say hello."}, {Role: "assistant", Text: "Hello."}},
		MaxTokens:    50,
		Temperature:  0.5,
		Models:       []string{"claude-opus-5-5", "claude"},
	}
	want := "System prompt:\nBe brief.\n\nUser:\nSay hello.\n\nAssistant:\nHello.\n\nMax tokens: 50\nTemperature: 0.5\nModels: claude-opus-5-5, claude"
	if got := samplingText(s); got != want {
		t.Errorf("samplingText = %q, want %q", got, want)
	}

	mainthread.Wait(func() {
		answered := false
		var answer *mcp.SampleAnswer
		d := newSamplingDialog(nil, "localhost", s)
		d.open(func(a *mcp.SampleAnswer) { answered, answer = true, a })

		// There has to be a reply.
		d.send()
		if answered || d.problem.Text() == "" {
			t.Errorf("sent without a reply: %+v", answer)
		}
		d.reply.SetPlainText("Hi!")
		d.send()
		if answer == nil || answer.Text != "Hi!" || answer.Model != "claude-opus-5-5" {
			t.Errorf("answer %+v", answer)
		}

		// Closing it any other way declines.
		d = newSamplingDialog(nil, "localhost", &mcp.Sampling{})
		answered = false
		close := d.open(func(a *mcp.SampleAnswer) { answered, answer = true, a })
		if d.model.Text() != samplingModel {
			t.Errorf("model %q", d.model.Text())
		}
		close()
		if !answered || answer != nil {
			t.Errorf("closed: %v %+v", answered, answer)
		}
	})
}
