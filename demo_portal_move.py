import sys
import gi
gi.require_version('GLib', '2.0')
gi.require_version('Gio', '2.0')
from gi.repository import GLib, Gio, GObject

bus = Gio.bus_get_sync(Gio.BusType.SESSION, None)

def call_sync(bus, bus_name, object_path, interface_name, method_name, parameters, reply_type):
    return bus.call_sync(
        bus_name, object_path, interface_name, method_name,
        parameters, reply_type,
        Gio.DBusCallFlags.NONE, -1, None
    )

portal_bus = 'org.freedesktop.portal.Desktop'
portal_path = '/org/freedesktop/portal/desktop'
portal_iface = 'org.freedesktop.portal.RemoteDesktop'
request_iface = 'org.freedesktop.portal.Request'

# Step 1: CreateSession
print("Creating session...")
res = call_sync(bus, portal_bus, portal_path, portal_iface, 'CreateSession',
                GLib.Variant('(a{sv})', ({},)), None)
request_path = res.unpack()[0]
print("Request path:", request_path)

# We actually need async to wait for Response signals.
