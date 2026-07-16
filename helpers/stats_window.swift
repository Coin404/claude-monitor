import Cocoa
import WebKit

// novascope-stats-window.swift — standalone helper that shows a stats HTML
// file in a native WKWebView window. Invoked by claude-monitor.

let app = NSApplication.shared
app.setActivationPolicy(.accessory)

guard CommandLine.arguments.count > 1 else {
    print("usage: novascope-stats-window <html-file-path>")
    exit(1)
}

let htmlPath = CommandLine.arguments[1]
let url = URL(fileURLWithPath: htmlPath)

// Window
let window = NSWindow(
    contentRect: NSRect(x: 0, y: 0, width: 520, height: 600),
    styleMask: [.titled, .closable],
    backing: .buffered,
    defer: false
)
window.title = "Novascope Stats"
window.center()
window.isReleasedWhenClosed = false
window.level = .floating

// WebView
let config = WKWebViewConfiguration()
let webView = WKWebView(frame: window.contentView!.bounds, configuration: config)
webView.autoresizingMask = [.width, .height]
window.contentView?.addSubview(webView)
webView.loadFileURL(url, allowingReadAccessTo: url.deletingLastPathComponent())

// Quit helper when window closes
class WindowDelegate: NSObject, NSWindowDelegate {
    func windowWillClose(_ notification: Notification) {
        NSApp.stop(nil)
    }
}
let delegate = WindowDelegate()
window.delegate = delegate

window.makeKeyAndOrderFront(nil)
NSApp.activate(ignoringOtherApps: true)
app.run()
