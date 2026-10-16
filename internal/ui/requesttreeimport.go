package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/openapi"
	"github.com/nomad-software/triggerapi/internal/postman"
)

// Importing documents of other programs into the request tree as folders of
// requests.

// importSource is a kind of file that requests can be imported from.
type importSource string

const (
	importOpenAPI importSource = "openapi"
	importPostman importSource = "postman"
)

// importOrder is the order the kinds of file are offered in.
var importOrder = []importSource{importOpenAPI, importPostman}

// importSources describes the kinds of file that can be imported.
var importSources = map[importSource]importKind[*collection.Folder]{
	importOpenAPI: {"OpenAPI", "OpenAPI documents (*.json *.yaml *.yml)", "#6ba539", drawDocument, openapi.Import},
	importPostman: {"Postman", "Postman collections (*.json)", "#ef5b25", drawPlane, postman.Import},
}

// newImportDialog returns a dialog which asks what kind of file to import.
func newImportDialog(parent *qt.QWidget) *choiceDialog[importSource] {
	return newSourceDialog(parent, "Choose what to import requests from.", importOrder, importSources)
}

// importDocument asks what kind of file to import, and for the file, and
// imports it.
func (t *requestTree) importDocument() {
	importDocument(t.chooseSource, t.chooseFile, t.importFile, t.warn)
}

// importFile adds a folder of requests made from the file at path, which is
// of the kind given, to the top level, and selects it. The folders inside it
// start off closed, as there can be a great many.
func (t *requestTree) importFile(source importSource, path string) error {
	folder, err := readImport(importSources[source], path)
	if err != nil {
		return err
	}
	t.adopt(folder)
	return nil
}
