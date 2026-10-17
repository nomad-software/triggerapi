package ui

import (
	"fmt"
	"reflect"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/network"
)

// How the window reaches servers: the network settings, loaded into the
// client every kind of request is sent with.

// askNetworkSecret asks for the passphrase of a key or the password of a
// proxy that the network settings need. Tests replace it rather than let the
// dialog open.
var askNetworkSecret = func(parent *qt.QWidget, secret network.Secret) (string, error) {
	title, label := "Client Certificate", "Passphrase of "+secret.Path
	if secret.Proxy {
		title, label = "Proxy", "Password for the proxy "+secret.Path
	}
	return askHidden(parent, title, label)
}

// loadNetwork loads network settings, asking for each passphrase or password
// they need that has not been given in this session. See network.Load.
func (w *Window) loadNetwork(config network.Config) (*network.Client, error) {
	if w.secrets == nil {
		w.secrets = map[network.Secret]string{}
	}
	client, err := network.Load(config, func(secret network.Secret) (string, error) {
		if value, ok := w.secrets[secret]; ok {
			return value, nil
		}
		value, err := askNetworkSecret(w.QWidget, secret)
		if err == nil {
			w.secrets[secret] = value
		}
		return value, err
	})
	if err != nil {
		err = fmt.Errorf("the network settings cannot all be used: %w", err)
	}
	return client, err
}

// useNetwork has requests sent with a client from now on. What was learnt of
// servers with the one before is forgotten, as they may be reached otherwise
// now, and its connections are closed.
func (w *Window) useNetwork(client *network.Client) {
	previous := w.net
	w.net, w.client = client, client.HTTP()
	if previous == nil {
		return
	}
	previous.CloseIdleConnections()
	w.closeMCP()
	w.schemas.clear()
	w.graphSchemas.clear()
	w.mcpSchemas.clear()
	w.request.updateCurl()
}

// applyNetwork puts network settings into effect, unless they cannot all be
// used, when those there are stay in effect and the reason is returned.
// Settings that could not all be used before are loaded again, as a
// passphrase may not have been given.
func (w *Window) applyNetwork(config network.Config) error {
	if reflect.DeepEqual(config, w.config.Network) && w.netErr == nil {
		return nil
	}
	client, err := w.loadNetwork(config)
	if err != nil {
		return err
	}
	w.config.Network, w.netErr = config, nil
	w.useNetwork(client)
	return nil
}
