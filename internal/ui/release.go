package ui

import "runtime"

// release frees a Qt value that a method returned, at once and on the GUI
// thread. Such a value is otherwise freed by a finalizer, which the garbage
// collector runs on a thread of its own. That is a crash waiting to happen
// for a QTextCursor, which is on the list its document keeps of its cursors:
// taking it off that list from another thread, while the GUI thread is
// changing the document, corrupts the list. Every text cursor that a method
// returns is to be released once it has been used.
func release[T interface{ Delete() }](value T) {
	runtime.SetFinalizer(value, nil)
	value.Delete()
}
