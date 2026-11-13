package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"

	"fmt"
	"strconv"
	"strings"

	"github.com/nomad-software/triggerapi/internal/mcp"
)

// askSampling shows what an MCP server asks a language model for in a
// dialog, in which the user writes the message to give it in place of the
// model's, and calls done with it once they have, or with nil if they
// decline. It returns what closes the dialog, as declined, if it is still
// open, as it is when the call is given up. Tests replace it rather than let
// the dialog open.
var askSampling = func(parent *qtwidgets.QWidget, server string, s *mcp.Sampling, done func(*mcp.SampleAnswer)) (close func()) {
	return newSamplingDialog(parent, server, s).open(done)
}

// samplingModel is the name of the model a reply written by hand is said to
// be from, if the server would like none in particular.
const samplingModel = "triggerapi"

// samplingDialog shows the messages an MCP server would have a language
// model answer, and takes the answer from the user.
type samplingDialog struct {
	*qtwidgets.QDialog
	reply   *qtwidgets.QPlainTextEdit
	model   *qtwidgets.QLineEdit
	problem statusLine
	// answer is the reply, which is nil until it is given: closing the
	// dialog any other way declines.
	answer *mcp.SampleAnswer
}

func newSamplingDialog(parent *qtwidgets.QWidget, server string, s *mcp.Sampling) *samplingDialog {
	d := &samplingDialog{
		QDialog: qtwidgets.NewQDialog(parent),
		reply:   qtwidgets.NewQPlainTextEdit(nil),
		model:   qtwidgets.NewQLineEdit(nil),
		problem: newStatusLine(),
	}
	d.SetWindowTitle("The Server Asks for a Message")
	d.Resize(560, 520)

	layout := qtwidgets.NewQVBoxLayout(d.AsQWidget())
	layout.SetContentsMargins(qtcore.QMargins{Left: 18, Top: 18, Right: 18, Bottom: 18})
	layout.SetSpacing(8)

	intro := qtwidgets.NewQLabelWithText(server+" asks for the message a language model would answer with. Write the reply to give it.", nil)
	intro.SetWordWrap(true)
	layout.AddWidget(intro.AsQWidget())

	asked := qtwidgets.NewQPlainTextEdit(nil)
	asked.SetReadOnly(true)
	asked.SetPlainText(samplingText(s))
	layout.AddWidgetFull(asked.AsQWidget(), 2, 0)

	layout.AddWidget(qtwidgets.NewQLabelWithText("Reply", nil).AsQWidget())
	layout.AddWidgetFull(d.reply.AsQWidget(), 1, 0)

	model := samplingModel
	if len(s.Models) > 0 {
		model = s.Models[0]
	}
	d.model.SetText(model)
	form := qtwidgets.NewQFormLayout(nil)
	form.AddRowText("Model", d.model.AsQWidget())
	layout.AddLayout(form.AsQLayout())
	layout.AddWidget(d.problem.AsQWidget())

	send := qtwidgets.NewQPushButtonWithText("Send Reply", nil)
	send.SetAutoDefault(false)
	send.OnClicked(func(_ bool) { d.send() })
	decline := qtwidgets.NewQPushButtonWithText("Decline", nil)
	decline.SetAutoDefault(false)
	decline.OnClicked(func(_ bool) { d.Reject() })

	buttons := qtwidgets.NewQHBoxLayout(nil)
	buttons.AddStretch()
	buttons.AddWidget(send.AsQWidget())
	buttons.AddWidget(decline.AsQWidget())
	layout.AddLayout(buttons.AsQLayout())

	d.reply.SetFocus()
	return d
}

// samplingText returns what a server asks a language model for as text: the
// system prompt and the messages, each under who it is from, and then what
// it would like of the model and its answer.
func samplingText(s *mcp.Sampling) string {
	var parts []string
	if s.SystemPrompt != "" {
		parts = append(parts, "System prompt:\n"+s.SystemPrompt)
	}
	for _, m := range s.Messages {
		role := m.Role
		if role != "" {
			role = strings.ToUpper(role[:1]) + role[1:]
		}
		parts = append(parts, role+":\n"+m.Text)
	}

	var wants []string
	if s.MaxTokens > 0 {
		wants = append(wants, fmt.Sprintf("Max tokens: %d", s.MaxTokens))
	}
	if s.Temperature != 0 {
		wants = append(wants, "Temperature: "+strconv.FormatFloat(s.Temperature, 'f', -1, 64))
	}
	if len(s.Models) > 0 {
		wants = append(wants, "Models: "+strings.Join(s.Models, ", "))
	}
	if len(s.StopSequences) > 0 {
		wants = append(wants, "Stop sequences: "+strings.Join(s.StopSequences, ", "))
	}
	if len(wants) > 0 {
		parts = append(parts, strings.Join(wants, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

// send answers with the reply, unless there is none.
func (d *samplingDialog) send() {
	text := d.reply.ToPlainText()
	if strings.TrimSpace(text) == "" {
		d.problem.set("Write the reply to give the server, or decline.")
		return
	}
	model := strings.TrimSpace(d.model.Text())
	if model == "" {
		model = samplingModel
	}
	d.answer = &mcp.SampleAnswer{Text: text, Model: model}
	d.Accept()
}

// open shows the dialog, which keeps the window from being used until it is
// answered, and calls done with the reply, or nil if it is declined, once it
// is. It returns what closes it, as declined, if it is still open.
func (d *samplingDialog) open(done func(*mcp.SampleAnswer)) func() {
	return openDialog(d.AsQDialog(), func() { done(d.answer) })
}
