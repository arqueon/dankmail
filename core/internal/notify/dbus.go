package notify

import (
	gosync "sync"

	"github.com/godbus/dbus/v5"
)

const (
	dbusDest = "org.freedesktop.Notifications"
	dbusPath = "/org/freedesktop/Notifications"
	appName  = "Dank Mail"
	appIcon  = "dankmail"
)

// DBusNotifier talks to org.freedesktop.Notifications directly, with
// inline actions dispatched back through per-notification callbacks.
type DBusNotifier struct {
	conn *dbus.Conn

	mu        gosync.Mutex
	callbacks map[uint32]func(actionKey string)
}

// NewDBus connects to the session bus and starts the signal dispatcher.
func NewDBus() (*DBusNotifier, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	n := &DBusNotifier{conn: conn, callbacks: map[uint32]func(string){}}

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbusPath),
		dbus.WithMatchInterface(dbusDest),
	); err != nil {
		return nil, err
	}
	ch := make(chan *dbus.Signal, 32)
	conn.Signal(ch)
	go n.dispatch(ch)
	return n, nil
}

func (n *DBusNotifier) dispatch(ch chan *dbus.Signal) {
	for sig := range ch {
		switch sig.Name {
		case dbusDest + ".ActionInvoked":
			if len(sig.Body) != 2 {
				continue
			}
			id, _ := sig.Body[0].(uint32)
			key, _ := sig.Body[1].(string)
			n.mu.Lock()
			cb := n.callbacks[id]
			n.mu.Unlock()
			if cb != nil {
				go cb(key)
			}
		case dbusDest + ".NotificationClosed":
			if len(sig.Body) < 1 {
				continue
			}
			id, _ := sig.Body[0].(uint32)
			n.mu.Lock()
			delete(n.callbacks, id)
			n.mu.Unlock()
		}
	}
}

// Send shows the notification. If nn.OnAction is set and the server
// supports actions, the inline buttons dispatch to it.
func (n *DBusNotifier) Send(nn Notification) (uint32, error) {
	var actions []string
	for _, a := range nn.Actions {
		actions = append(actions, a.Key, a.Label)
	}
	hints := map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(nn.Urgency)),
		"desktop-entry": dbus.MakeVariant("org.arqueon.dankmail"),
	}
	switch nn.Sound {
	case "":
		// Server default.
	case "none":
		hints["suppress-sound"] = dbus.MakeVariant(true)
	default:
		hints["sound-name"] = dbus.MakeVariant(nn.Sound)
	}

	obj := n.conn.Object(dbusDest, dbusPath)
	call := obj.Call(dbusDest+".Notify", 0,
		appName, uint32(0), appIcon,
		nn.Summary, nn.Body,
		actions, hints, int32(-1),
	)
	if call.Err != nil {
		return 0, call.Err
	}
	var id uint32
	if err := call.Store(&id); err != nil {
		return 0, err
	}
	if nn.OnAction != nil {
		n.mu.Lock()
		n.callbacks[id] = nn.OnAction
		n.mu.Unlock()
	}
	return id, nil
}

func (n *DBusNotifier) Close(id uint32) error {
	return n.conn.Object(dbusDest, dbusPath).
		Call(dbusDest+".CloseNotification", 0, id).Err
}
