package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"strings"

	"github.com/nomad-software/triggerapi/internal/network"
)

// The choices of the proxy on the Network tab of the settings, in the order
// they are offered.
const (
	proxyEnvironment = iota
	proxyNone
	proxyAddress
)

// The choices of the least version of TLS, in the order they are offered:
// Go's own least, which is TLS 1.2, and TLS 1.3.
var minTLSChoices = []struct{ label, value string }{
	{"TLS 1.2", ""},
	{"TLS 1.3", network.TLS13},
}

// networkTab is the Network tab of the settings dialog: the proxy, the least
// version of TLS, and a list of hosts with what is particular to each, which
// is edited in the form beside the list for the host selected in it.
type networkTab struct {
	root *qtwidgets.QWidget

	proxyMode *qtwidgets.QComboBox
	proxy     *qtwidgets.QLineEdit
	noProxy   *qtwidgets.QLineEdit
	minTLS    *qtwidgets.QComboBox

	// hosts are the entries being edited, in the order of the list.
	hosts []network.Host
	list  *qtwidgets.QListWidget
	// form holds the fields of the host selected, and is disabled while
	// none is.
	form     *qtwidgets.QWidget
	pattern  *qtwidgets.QLineEdit
	ca       *qtwidgets.QLineEdit
	cert     *qtwidgets.QLineEdit
	key      *qtwidgets.QLineEdit
	insecure *qtwidgets.QCheckBox
	address  *qtwidgets.QLineEdit
	remove   *qtwidgets.QPushButton
	// updating is set while the form is filled, which is not an edit.
	updating bool
	// choose asks for a file, offering the path given; tests replace it.
	choose func(caption, start string) string
	// browse is the button beside each field of a file.
	browse map[*qtwidgets.QLineEdit]*qtwidgets.QPushButton
}

// noHostName is what the list shows for a host whose name is empty.
const noHostName = "(no host)"

func newNetworkTab(parent *qtwidgets.QWidget, current network.Config) *networkTab {
	t := &networkTab{
		proxyMode: qtwidgets.NewQComboBox(nil),
		proxy:     qtwidgets.NewQLineEdit(nil),
		noProxy:   qtwidgets.NewQLineEdit(nil),
		minTLS:    qtwidgets.NewQComboBox(nil),
		list:      qtwidgets.NewQListWidget(nil),
		form:      qtwidgets.NewQWidget(nil),
		pattern:   qtwidgets.NewQLineEdit(nil),
		ca:        qtwidgets.NewQLineEdit(nil),
		cert:      qtwidgets.NewQLineEdit(nil),
		key:       qtwidgets.NewQLineEdit(nil),
		insecure:  qtwidgets.NewQCheckBoxWithText("Do not check the server's certificate", nil),
		address:   qtwidgets.NewQLineEdit(nil),
		hosts:     append([]network.Host(nil), current.Hosts...),
		browse:    map[*qtwidgets.QLineEdit]*qtwidgets.QPushButton{},
	}
	t.choose = func(caption, start string) string {
		if start == "" {
			start = qtcore.QDirHomePath()
		}
		return askOpenFile(parent, caption, start, "")
	}

	t.proxyMode.AddItems([]string{"From the environment", "None", "This proxy"})
	switch current.Proxy {
	case network.ProxyEnvironment:
		t.proxyMode.SetCurrentIndex(proxyEnvironment)
	case network.ProxyNone:
		t.proxyMode.SetCurrentIndex(proxyNone)
	default:
		t.proxyMode.SetCurrentIndex(proxyAddress)
		t.proxy.SetText(current.Proxy)
	}
	t.proxy.SetPlaceholderText("Such as http://proxy:3128 or socks5://user@proxy:1080")
	t.proxy.SetEnabled(t.proxyMode.CurrentIndex() == proxyAddress)
	t.proxyMode.OnCurrentIndexChanged(func(i int) { t.proxy.SetEnabled(i == proxyAddress) })
	proxyRow := qtwidgets.NewQHBoxLayout(nil)
	proxyRow.AddWidget(t.proxyMode.AsQWidget())
	proxyRow.AddWidgetFull(t.proxy.AsQWidget(), 1, 0)

	t.noProxy.SetText(strings.Join(current.NoProxy, ", "))
	t.noProxy.SetPlaceholderText("Such as localhost, .internal.example.com")

	for _, choice := range minTLSChoices {
		t.minTLS.AddItem(choice.label)
	}
	if current.MinTLS == network.TLS13 {
		t.minTLS.SetCurrentIndex(1)
	}

	proxyNote := qtwidgets.NewQLabelWithText("A proxy can name a user; the password is asked for once a session and never saved. "+
		"Hosts under Not for, and the hosts inside them, are reached directly.", nil)
	proxyNote.SetWordWrap(true)
	proxyNote.SetEnabled(false)

	// The settings have buttons of the style, as the rest of the dialog
	// does, not the icon buttons of the window.
	add := qtwidgets.NewQPushButtonWithText("Add", nil)
	add.OnClicked(func(_ bool) { t.add() })
	t.remove = qtwidgets.NewQPushButtonWithText("Remove", nil)
	t.remove.OnClicked(func(_ bool) { t.removeSelected() })
	for i := range t.hosts {
		t.list.AddItemText(hostLabel(t.hosts[i]))
	}
	t.list.OnCurrentRowChanged(func(int) { t.fill() })

	listColumn := qtwidgets.NewQVBoxLayout(nil)
	listColumn.AddWidgetFull(t.list.AsQWidget(), 1, 0)
	buttons := qtwidgets.NewQHBoxLayout(nil)
	buttons.AddWidget(add.AsQWidget())
	buttons.AddWidget(t.remove.AsQWidget())
	buttons.AddStretch()
	listColumn.AddLayout(buttons.AsQLayout())

	t.pattern.SetPlaceholderText("Such as api.example.com, *.example.com or *")
	t.ca.SetPlaceholderText("Trust only the authorities in this file, in PEM")
	t.cert.SetPlaceholderText("A certificate in PEM, or a PKCS #12 file")
	t.key.SetPlaceholderText("Its private key in PEM, for a certificate in PEM")
	t.address.SetPlaceholderText("Such as 10.0.0.5 or 10.0.0.5:8443")
	form := qtwidgets.NewQFormLayout(t.form)
	form.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: 0})
	form.AddRowText("Host", t.pattern.AsQWidget())
	form.AddRowTextLayout("Trusted CA", t.fileRow(t.ca, "Certificate Authority"))
	form.AddRowTextLayout("Client certificate", t.fileRow(t.cert, "Client Certificate"))
	form.AddRowTextLayout("Key", t.fileRow(t.key, "Private Key"))
	form.AddRowText("", t.insecure.AsQWidget())
	form.AddRowText("Connect to", t.address.AsQWidget())
	for _, edit := range []*qtwidgets.QLineEdit{t.pattern, t.ca, t.cert, t.key, t.address} {
		edit.OnTextChanged(func(string) { t.edited() })
	}
	t.insecure.OnToggled(func(bool) { t.edited() })

	hostsRow := qtwidgets.NewQHBoxLayout(nil)
	hostsRow.AddLayoutFull(listColumn.AsQLayout(), 1)
	hostsRow.AddWidgetFull(t.form, 3, 0)

	hostsNote := qtwidgets.NewQLabelWithText("A request uses the most particular host that matches its own: a name before *.name, and *.name before *. "+
		"A trusted CA replaces the system's authorities for it. A passphrase is asked for once a session and never saved. "+
		"These settings belong to this computer, not to a workspace.", nil)
	hostsNote.SetWordWrap(true)
	hostsNote.SetEnabled(false)

	// The page is a column, as notes that wrap are given too little height
	// in the rows of a form.
	proxyForm := qtwidgets.NewQFormLayout(nil)
	proxyForm.AddRowTextLayout("Proxy", proxyRow.AsQLayout())
	proxyForm.AddRowText("Not for", t.noProxy.AsQWidget())
	proxyForm.AddRowText("Least TLS version", t.minTLS.AsQWidget())

	t.root = qtwidgets.NewQWidget(nil)
	page := qtwidgets.NewQVBoxLayout(t.root)
	page.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	page.AddWidget(sectionHeading("Connection").AsQWidget())
	page.AddLayout(proxyForm.AsQLayout())
	page.AddWidget(proxyNote.AsQWidget())
	page.AddSpacing(12)
	page.AddWidget(sectionHeading("Hosts").AsQWidget())
	page.AddLayoutFull(hostsRow.AsQLayout(), 1)
	page.AddWidget(hostsNote.AsQWidget())
	// The least height of a page is worked out before the width of its notes
	// is known, which makes it too little for them once they wrap, so the
	// page asks for the height it needs at the width it is given.
	t.root.OverrideResizeEvent(func(super func(*qtgui.QResizeEvent), event *qtgui.QResizeEvent) {
		super(event)
		if need := t.root.HeightForWidth(t.root.Width()); need > t.root.MinimumHeight() {
			t.root.SetMinimumHeight(need)
		}
	})

	if len(t.hosts) > 0 {
		t.list.SetCurrentRow(0)
	}
	t.fill()
	return t
}

// fileRow lays out a path field beside a button to browse for the file,
// which is kept in browse.
func (t *networkTab) fileRow(edit *qtwidgets.QLineEdit, caption string) *qtwidgets.QLayout {
	button := browseButton(edit, func() string { return t.choose(caption, strings.TrimSpace(edit.Text())) })
	t.browse[edit] = button
	return pathRow(edit, button)
}

// hostLabel returns what the list shows for a host.
func hostLabel(h network.Host) string {
	if name := strings.TrimSpace(h.Pattern); name != "" {
		return name
	}
	return noHostName
}

// fill shows the host selected in the form, which is disabled and empty if
// none is.
func (t *networkTab) fill() {
	t.updating = true
	defer func() { t.updating = false }()

	row := t.list.CurrentRow()
	var h network.Host
	selected := row >= 0 && row < len(t.hosts)
	if selected {
		h = t.hosts[row]
	}
	t.form.SetEnabled(selected)
	t.remove.SetEnabled(selected)
	t.pattern.SetText(h.Pattern)
	t.ca.SetText(h.CA)
	t.cert.SetText(h.Cert)
	t.key.SetText(h.Key)
	t.insecure.SetChecked(h.Insecure)
	t.address.SetText(h.Address)
}

// edited records what the form holds as the host selected.
func (t *networkTab) edited() {
	row := t.list.CurrentRow()
	if t.updating || row < 0 || row >= len(t.hosts) {
		return
	}
	t.hosts[row] = network.Host{
		Pattern:  strings.TrimSpace(t.pattern.Text()),
		CA:       strings.TrimSpace(t.ca.Text()),
		Cert:     strings.TrimSpace(t.cert.Text()),
		Key:      strings.TrimSpace(t.key.Text()),
		Insecure: t.insecure.IsChecked(),
		Address:  strings.TrimSpace(t.address.Text()),
	}
	t.list.Item(row).SetText(hostLabel(t.hosts[row]))
}

// add adds a host and selects it, with the keyboard in its name.
func (t *networkTab) add() {
	t.hosts = append(t.hosts, network.Host{})
	t.list.AddItemText(noHostName)
	t.list.SetCurrentRow(len(t.hosts) - 1)
	t.pattern.SetFocus()
}

// removeSelected removes the host selected.
func (t *networkTab) removeSelected() {
	row := t.list.CurrentRow()
	if row < 0 || row >= len(t.hosts) {
		return
	}
	t.hosts = append(t.hosts[:row], t.hosts[row+1:]...)
	t.list.TakeItem(row).Delete()
	t.fill()
}

// config returns the network settings the tab holds. A host left with
// nothing in it is left out.
func (t *networkTab) config() network.Config {
	var c network.Config
	switch t.proxyMode.CurrentIndex() {
	case proxyNone:
		c.Proxy = network.ProxyNone
	case proxyAddress:
		// An address left empty is the environment's proxy.
		c.Proxy = strings.TrimSpace(t.proxy.Text())
	}
	// None is nil, as in the settings, so that the dialog left as it was
	// gives them back unchanged.
	if names := strings.FieldsFunc(t.noProxy.Text(), func(r rune) bool { return r == ',' || r == ' ' }); len(names) > 0 {
		c.NoProxy = names
	}
	c.MinTLS = minTLSChoices[max(t.minTLS.CurrentIndex(), 0)].value
	for _, h := range t.hosts {
		if h != (network.Host{}) {
			c.Hosts = append(c.Hosts, h)
		}
	}
	return c
}
