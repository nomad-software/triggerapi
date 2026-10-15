package ui

import (
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

const (
	colEndpointChosen = iota
	colEndpointName
)

// The pages of an endpointTable.
const (
	endpointPageHint = iota
	endpointPageTable
)

// endpointTable is the Service Definitions tab of the gRPC panel. It lists the
// methods of the server by their short names, under a heading for each
// service, with a checkbox against each, and has a button which makes a
// request of each one that is ticked. A search box above it narrows the list,
// as the one above the requests of the sidebar does.
//
// A request that is in a collection of such requests has the collection
// edited in place of another being made: the methods it already has requests
// for are ticked and cannot be unticked, and the button adds requests for the
// others that are ticked. See setExisting.
type endpointTable struct {
	root *qt.QWidget
	// search narrows the table to the methods that match what is typed in
	// it. See applySearch.
	search *qt.QLineEdit
	pages  *qt.QStackedWidget
	table  *qt.QTableWidget
	// toggle ticks every method on show, or none of them if they all are.
	toggle *qt.QPushButton
	create *qt.QPushButton

	// names holds the full name of the method on each row, as
	// package.Service/Method. It is empty for the heading rows.
	names    []string
	updating bool

	// existing are the methods there are requests for already, which stay
	// ticked. editing is set when the requests to be made are to join them.
	existing map[string]bool
	editing  bool

	// onCreate is called with the full names of the methods that are
	// ticked, but for those there are requests for already, when the user
	// asks for requests to be made of them.
	onCreate func(methods []string)
}

func newEndpointTable() *endpointTable {
	e := &endpointTable{
		root:   qt.NewQWidget2(),
		search: qt.NewQLineEdit2(),
		pages:  qt.NewQStackedWidget2(),
		table:  qt.NewQTableWidget3(0, 2),
		toggle: qt.NewQPushButton2(),
		create: qt.NewQPushButton3("Create Requests"),
	}

	// The columns need no names: there is a box and what it is for.
	e.table.HorizontalHeader().SetVisible(false)
	e.table.VerticalHeader().SetVisible(false)
	e.table.SetSelectionMode(qt.QAbstractItemView__NoSelection)
	e.table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	header := e.table.HorizontalHeader()
	header.SetSectionResizeMode2(colEndpointChosen, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colEndpointName, qt.QHeaderView__Stretch)
	e.table.OnItemChanged(func(*qt.QTableWidgetItem) {
		if !e.updating {
			e.refresh()
		}
	})

	e.search.SetPlaceholderText("Search endpoints")
	e.search.SetClearButtonEnabled(true)
	e.search.OnTextChanged(func(string) {
		e.applySearch()
		e.refresh()
	})

	e.toggle.OnClicked(func() { e.setAll(!e.all()) })
	e.create.OnClicked(func() {
		if methods := e.added(); len(methods) > 0 && e.onCreate != nil {
			e.onCreate(methods)
		}
	})

	buttons := qt.NewQHBoxLayout2()
	buttons.AddWidget(e.toggle.QWidget)
	buttons.AddWidget(e.create.QWidget)
	buttons.AddStretch()

	// The order matches the page constants.
	e.pages.AddWidget(newHint("The endpoints of the server are listed here once they have been loaded."))
	e.pages.AddWidget(e.table.QWidget)

	layout := qt.NewQVBoxLayout(e.root)
	layout.AddWidget(e.search.QWidget)
	layout.AddWidget2(e.pages.QWidget, 1)
	layout.AddLayout(buttons.QLayout)

	e.setMethods(nil)

	return e
}

// setMethods lists the methods, which are given by their full names and must
// be sorted so that those of a service are together. None of them is ticked,
// and there are taken to be requests for none of them.
func (e *endpointTable) setMethods(methods []string) {
	e.updating = true
	e.existing, e.editing = nil, false

	e.table.SetRowCount(0)
	e.table.ClearSpans()
	e.names = nil

	service := ""
	for _, name := range methods {
		parent, method, _ := strings.Cut(name, "/")
		if parent != service {
			service = parent

			// A heading is as wide as the table and cannot be ticked.
			heading := qt.NewQTableWidgetItem2(service)
			heading.SetFlags(qt.ItemIsEnabled)
			font := heading.Font()
			font.SetBold(true)
			heading.SetFont(font)

			row := e.table.RowCount()
			e.table.InsertRow(row)
			e.table.SetItem(row, colEndpointChosen, heading)
			e.table.SetSpan(row, colEndpointChosen, 1, 2)
			e.names = append(e.names, "")
		}

		check := qt.NewQTableWidgetItem()
		check.SetFlags(qt.ItemIsEnabled | qt.ItemIsUserCheckable)
		check.SetCheckState(qt.Unchecked)
		label := qt.NewQTableWidgetItem2(method)
		label.SetFlags(qt.ItemIsEnabled)

		row := e.table.RowCount()
		e.table.InsertRow(row)
		e.table.SetItem(row, colEndpointChosen, check)
		e.table.SetItem(row, colEndpointName, label)
		e.names = append(e.names, name)
	}

	e.updating = false

	e.pages.SetCurrentIndex(endpointPageTable)
	if len(methods) == 0 {
		e.pages.SetCurrentIndex(endpointPageHint)
	}
	// There is nothing to search until there are methods.
	e.search.SetEnabled(len(methods) > 0)
	e.applySearch()
	e.refresh()
}

// applySearch hides the methods that do not match what is in the search
// box, and the headings of the services none of whose methods are left. A
// method matches if every word typed is in its name or that of its service,
// ignoring case, so the name of a service finds all of its methods. Rows
// are hidden and not removed, and a method that is hidden stays ticked if it
// was.
func (e *endpointTable) applySearch() {
	words := strings.Fields(strings.ToLower(e.search.Text()))

	heading, any := -1, false
	show := func() {
		if heading >= 0 {
			e.table.SetRowHidden(heading, !any)
		}
	}
	for row, name := range e.names {
		if name == "" {
			show()
			heading, any = row, false
			continue
		}

		found := true
		for _, word := range words {
			found = found && strings.Contains(strings.ToLower(name), word)
		}
		e.table.SetRowHidden(row, !found)
		any = any || found
	}
	show()
}

// setExisting says which methods there are requests for already, in the
// collection the request on show is in, and whether it is in one at all.
// Those methods are ticked and cannot be unticked. One there is no longer a
// request for is unticked, and the rest are left as the user has them.
func (e *endpointTable) setExisting(existing map[string]bool, editing bool) {
	same := editing == e.editing && len(existing) == len(e.existing)
	for name := range existing {
		same = same && e.existing[name]
	}
	if same {
		return
	}

	e.updating = true
	for row, name := range e.names {
		if name == "" || existing[name] == e.existing[name] {
			continue
		}
		// A box that cannot be changed is drawn as one that is disabled.
		item := e.table.Item(row, colEndpointChosen)
		if existing[name] {
			item.SetFlags(qt.NoItemFlags)
		} else {
			item.SetFlags(qt.ItemIsEnabled | qt.ItemIsUserCheckable)
		}
		item.SetCheckState(checkState(existing[name]))
	}
	e.updating = false

	e.existing, e.editing = existing, editing
	e.refresh()
}

// hasService reports whether any of the methods is of a service.
func (e *endpointTable) hasService(service string) bool {
	return slices.ContainsFunc(e.names, func(name string) bool { return strings.HasPrefix(name, service+"/") })
}

// chosen returns the full names of the methods that are ticked.
func (e *endpointTable) chosen() []string {
	var chosen []string
	for row, name := range e.names {
		if name != "" && e.table.Item(row, colEndpointChosen).CheckState() == qt.Checked {
			chosen = append(chosen, name)
		}
	}
	return chosen
}

// added returns the full names of the methods that are ticked and have no
// request already, which are those the user ticked.
func (e *endpointTable) added() []string {
	return slices.DeleteFunc(e.chosen(), func(name string) bool { return e.existing[name] })
}

// free returns the rows of the methods on show that can be ticked and
// unticked, which are those the toggle is for: not the ones a search has
// hidden, nor the ones there are requests for already.
func (e *endpointTable) free() []int {
	var rows []int
	for row, name := range e.names {
		if name != "" && !e.existing[name] && !e.table.IsRowHidden(row) {
			rows = append(rows, row)
		}
	}
	return rows
}

// all reports whether every method on show that can be ticked is, which
// none is if there are none.
func (e *endpointTable) all() bool {
	rows := e.free()
	for _, row := range rows {
		if e.table.Item(row, colEndpointChosen).CheckState() != qt.Checked {
			return false
		}
	}
	return len(rows) > 0
}

// setAll ticks every method on show, or none of them but for those which
// stay ticked. The ones a search has hidden are left as they are.
func (e *endpointTable) setAll(chosen bool) {
	e.updating = true
	for _, row := range e.free() {
		e.table.Item(row, colEndpointChosen).SetCheckState(checkState(chosen))
	}
	e.updating = false

	e.refresh()
}

// refresh makes the buttons say and allow what goes with the methods that
// are ticked.
func (e *endpointTable) refresh() {
	e.toggle.SetText("Select All")
	if e.all() {
		e.toggle.SetText("Select None")
	}
	e.toggle.SetEnabled(len(e.free()) > 0)

	e.create.SetText("Create Requests")
	if e.editing {
		e.create.SetText("Update Requests")
	}
	e.create.SetEnabled(len(e.added()) > 0)
}

// endpointFolder returns a folder of gRPC requests, one for each of the
// methods, which are given by their full names, in a folder inside it for
// each service. A request is named after its method and is for the server
// at url, with no message and everything else, TLS among it, as it is in a
// new request.
func endpointFolder(name, url string, methods []string) *collection.Folder {
	folder := &collection.Folder{Name: name}
	addEndpoints(folder, url, methods)
	return folder
}

// endpointsOf returns the methods a folder made by endpointFolder has
// requests for. They are found by what the requests call, and not by their
// names or that of the folder: a gRPC request counts if the folder it is in,
// inside this one, is named after the service of its method.
func endpointsOf(folder *collection.Folder) map[string]bool {
	found := map[string]bool{}
	for _, service := range folder.Folders {
		for _, r := range service.Requests {
			if r.Type == collection.TypeGRPC && strings.HasPrefix(r.GRPC.Method, service.Name+"/") {
				found[r.GRPC.Method] = true
			}
		}
	}
	return found
}

// addEndpoints adds a request for each of the methods to a folder as
// endpointFolder makes them, in the folder there is for its service or a new
// one. A method the folder has a request for is not given another.
func addEndpoints(folder *collection.Folder, url string, methods []string) {
	existing := endpointsOf(folder)
	for _, full := range methods {
		if existing[full] {
			continue
		}
		existing[full] = true
		parent, method, _ := strings.Cut(full, "/")

		index := slices.IndexFunc(folder.Folders, func(f *collection.Folder) bool { return f.Name == parent })
		if index < 0 {
			folder.Folders = append(folder.Folders, &collection.Folder{Name: parent})
			index = len(folder.Folders) - 1
		}
		service := folder.Folders[index]
		service.Requests = append(service.Requests, collection.NewGRPC(method, rpc.Request{URL: url, Method: full}))
	}
}
