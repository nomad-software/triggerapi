package ui

import (
	"strconv"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// How a response panel shows a stream of server-sent events: the events, as
// they come, oldest first, in place of a body, and how the stream went in the
// bar beneath.

// The values of the Stream field of the bar beneath a stream of events.
const (
	streamOpen  = "Open"
	streamEnded = "Ended"
)

// showStream shows a response that is a stream of server-sent events. While
// it is live, which is while it is still coming, the events are added to
// those on show, which are followed if the newest was in view, and once it
// has ended it is shown whole, as any other response is.
func (p *responsePanel) showStream(res *rest.Response, live bool) {
	events := res.StreamEvents()
	following := atEnd(p.body)
	switch {
	case p.streaming && p.streamed > 0 && p.streamed <= len(events):
		// Only the events that are new are added, as the stream can be
		// long.
		for _, e := range events[p.streamed:] {
			appendEntry(p.body, rest.FormatEvent(e))
		}
		if !live {
			p.find.search()
		}
	default:
		p.setBody(rest.EventLog(events), false)
		following = live
	}
	p.streamed, p.streaming = len(events), live
	if following {
		scrollToEnd(p.body)
	}

	p.captureDoc = res.Document()
	p.setContent(res.Body, responseFileName(res.Headers.Get("Content-Type")))
	p.setHeaders(headerRows(res.Headers, ""))

	state := field{label: "Stream", value: streamOpen}
	problem := field{fieldError, "", true}
	switch {
	case live:
	case res.Interrupted == rest.StreamStopped:
		state.value = rest.StreamStopped
	case res.Interrupted != "":
		state.value = streamEnded
		problem.value = res.Interrupted
	default:
		state.value = streamEnded
	}
	fields := []field{
		{fieldCode, res.Status, res.StatusCode >= 400},
		problem,
		state,
		{label: "Events", value: strconv.Itoa(len(events))},
	}
	if len(events) > 0 && events[0].At > 0 {
		fields = append(fields, field{label: "First event", value: formatDuration(events[0].At)})
	}
	p.setFields(append(fields, httpFields(res)...)...)
	if !live {
		p.showTiming(res.Timing, res.Duration)
	}
}
