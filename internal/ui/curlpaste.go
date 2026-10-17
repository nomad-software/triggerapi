package ui

import "github.com/nomad-software/triggerapi/internal/rest"

// pasteCurl makes a curl command pasted into the URL field of a request that
// is not saved the request, replacing what it was, and reports if the text
// was taken. Anything else, or anything pasted into a saved request, is
// pasted as usual. A command that cannot be made into a request is not
// pasted, and the user is told why.
func (p *requestPanel) pasteCurl(text string) bool {
	if !p.unsaved || !rest.IsCurl(text) {
		return false
	}
	r, err := rest.ParseCurl(text)
	if err != nil {
		p.warnPaste("This curl command cannot be made into a request: " + err.Error())
		return true
	}
	p.load(r)
	p.changed()
	return true
}
