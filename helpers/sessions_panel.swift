import Cocoa
import SwiftUI

// novascope-panel-helper — standalone SwiftUI helper that shows a glass-style
// sessions overview panel. Reads /tmp/claude-monitor-sessions.json (written by
// the Go process every 200ms) and renders session cards with one-click terminal
// activation.

// MARK: - Data model

struct SessionInfo: Codable, Equatable, Identifiable {
    var id: Int { pid }
    let pid: Int
    let color: String
    let project: String
    let terminal: String
    let statusKey: String
    let statusLabel: String
    let colorHex: String
}

struct SessionsSnapshot: Codable {
    let sessions: [SessionInfo]
    let timestamp: String
    let count: Int
    let usage: UsageSummary?
}

// MARK: - Usage data model

struct UsageSummary: Codable, Equatable {
    let updatedAt: String
    let balance: BalanceInfo?
    let providers: [ProviderUsage]?
}

struct BalanceInfo: Codable, Equatable {
    let totalBalance: Double
    let todaySpending: Double
    let currency: String
}

struct ProviderUsage: Codable, Equatable, Identifiable {
    var id: String { name }
    let name: String
    let inputTokens: Int64
    let outputTokens: Int64
    let totalCost: Double
    let requestCount: Int64
}

// MARK: - ViewModel

final class SessionsViewModel: ObservableObject {
    @Published var sessions: [SessionInfo] = []
    @Published var usage: UsageSummary?
    @Published var lastUpdate: Date?
    @Published var isStale = false
    private var hasEverHadSessions = false

    private var timer: Timer?
    private let jsonPath = "/tmp/claude-monitor-sessions.json"
    private let staleThreshold: TimeInterval = 30

    func startPolling() {
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
            self?.refresh()
        }
    }

    func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func refresh() {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: jsonPath)) else { return }
        guard let snap = try? JSONDecoder().decode(SessionsSnapshot.self, from: data) else { return }
        let newSessions = snap.sessions
        let newUsage = snap.usage
        if newSessions != sessions {
            sessions = newSessions
        }
        if newUsage != usage {
            usage = newUsage
        }
        if !newSessions.isEmpty {
            hasEverHadSessions = true
        }
        lastUpdate = Date()

        // Auto-close when all sessions end (only after we've seen at least one)
        if hasEverHadSessions && newSessions.isEmpty {
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                NSApp.stop(nil)
            }
        }

        // Check staleness
        if !sessions.isEmpty, let last = lastUpdate, Date().timeIntervalSince(last) > staleThreshold {
            isStale = true
        } else {
            isStale = false
        }
    }

    var timeSinceUpdate: String {
        guard let last = lastUpdate else { return "Waiting..." }
        let sec = Int(Date().timeIntervalSince(last))
        if sec < 5 { return "Just now" }
        if isStale { return "Stale (\(sec)s)" }
        return "Updated \(sec)s ago"
    }
}

// MARK: - Glass background

struct VisualEffectView: NSViewRepresentable {
    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = .hudWindow
        view.blendingMode = .behindWindow
        view.state = .active
        view.wantsLayer = true
        view.layer?.cornerRadius = 16
        view.layer?.masksToBounds = true
        return view
    }

    func updateNSView(_ nsView: NSVisualEffectView, context: Context) {}
}

// MARK: - Session card row

struct SessionCard: View {
    let session: SessionInfo
    @State private var isHovered = false

    var body: some View {
        HStack(spacing: 12) {
            // Colored dot
            Circle()
                .fill(Color(hex: session.colorHex))
                .frame(width: 12, height: 12)
                .shadow(color: Color(hex: session.colorHex).opacity(0.5), radius: 4)

            VStack(alignment: .leading, spacing: 2) {
                Text(session.project)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(.primary)
                    .lineLimit(1)
                Text(session.terminal)
                    .font(.system(size: 11))
                    .foregroundColor(.secondary)
                    .lineLimit(1)
            }

            Spacer()

            Text(session.statusLabel)
                .font(.system(size: 11, weight: .medium))
                .foregroundColor(Color(hex: session.colorHex))
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .background(
                    RoundedRectangle(cornerRadius: 4)
                        .fill(Color(hex: session.colorHex).opacity(0.12))
                )
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: 10)
                .fill(isHovered
                    ? Color.primary.opacity(0.08)
                    : Color.primary.opacity(0.04))
        )
        .scaleEffect(isHovered ? 1.02 : 1.0)
        .shadow(color: isHovered ? Color.black.opacity(0.15) : Color.clear, radius: 6, y: 2)
        .animation(.easeOut(duration: 0.15), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
        }
        .onTapGesture {
            selectSession(session.pid)
            activateTerminal(session.terminal)
        }
    }

    private func selectSession(_ pid: Int) {
        let path = "/tmp/claude-monitor-selected-pid"
        try? String(pid).write(toFile: path, atomically: true, encoding: .utf8)
    }

    private func activateTerminal(_ appName: String) {
        guard !appName.isEmpty else { return }
        let workspace = NSWorkspace.shared
        let lowerName = appName.lowercased()
        for app in workspace.runningApplications {
            if app.localizedName?.lowercased() == lowerName {
                app.unhide()
                app.activate(options: .activateAllWindows)
                return
            }
        }
    }
}

// MARK: - Empty state

struct EmptyState: View {
    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "circle.dotted")
                .font(.system(size: 40))
                .foregroundColor(.secondary.opacity(0.5))
            Text("No Active Sessions")
                .font(.system(size: 15, weight: .semibold))
                .foregroundColor(.secondary)
            Text("Claude Code sessions will appear here")
                .font(.system(size: 12))
                .foregroundColor(.secondary.opacity(0.7))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Usage card

struct UsageCard: View {
    let usage: UsageSummary

    var body: some View {
        VStack(spacing: 0) {
            Divider()
                .opacity(0.3)

            VStack(spacing: 8) {
                HStack {
                    Text("Today's Usage")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(.secondary)
                    Spacer()
                }

                // Balance from DeepSeek official API
                if let balance = usage.balance {
                    HStack(spacing: 8) {
                        Text("DeepSeek")
                            .font(.system(size: 12, weight: .medium))
                            .foregroundColor(.primary)
                        Spacer()
                        Text("余额 ¥\(String(format: "%.2f", balance.totalBalance))")
                            .font(.system(size: 11))
                            .foregroundColor(.secondary)
                    }
                }

                // Spending + tokens on one row
                if let balance = usage.balance {
                    HStack(spacing: 8) {
                        Spacer()
                        Text("¥\(String(format: "%.2f", balance.todaySpending))")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundColor(.primary)
                            .monospacedDigit()
                        if let providers = usage.providers, let first = providers.first {
                            HStack(spacing: 4) {
                                TokenLabel(icon: "arrow.down", value: formatTokens(first.inputTokens))
                                TokenLabel(icon: "arrow.up", value: formatTokens(first.outputTokens))
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
        }
    }
}

struct ProviderRow: View {
    let provider: ProviderUsage

    var body: some View {
        HStack(spacing: 8) {
            Text(provider.name)
                .font(.system(size: 12, weight: .medium))
                .foregroundColor(.primary)
                .lineLimit(1)

            Spacer()

            HStack(spacing: 4) {
                TokenLabel(icon: "arrow.down", value: formatTokens(provider.inputTokens))
                TokenLabel(icon: "arrow.up", value: formatTokens(provider.outputTokens))
            }
        }
    }
}

struct TokenLabel: View {
    let icon: String
    let value: String

    var body: some View {
        HStack(spacing: 1) {
            Image(systemName: icon)
                .font(.system(size: 8, weight: .medium))
                .foregroundColor(.secondary.opacity(0.6))
            Text(value)
                .font(.system(size: 10, weight: .medium))
                .foregroundColor(.secondary)
                .monospacedDigit()
        }
    }
}

private let usdToCnyRate: Double = {
    if let s = ProcessInfo.processInfo.environment["CC_SWITCH_USD_CNY_RATE"],
       let r = Double(s), r > 0 {
        return r
    }
    return 7.25
}()

private func formatTokens(_ tokens: Int64) -> String {
    if tokens >= 1_000_000 {
        return String(format: "%.1fM", Double(tokens) / 1_000_000.0)
    } else if tokens >= 1_000 {
        return String(format: "%.1fK", Double(tokens) / 1_000.0)
    } else {
        return "\(tokens)"
    }
}

// MARK: - Footer

struct FooterView: View {
    let text: String
    let isStale: Bool

    var body: some View {
        Text(text)
            .font(.system(size: 10))
            .foregroundColor(isStale ? .orange : .secondary.opacity(0.7))
            .padding(.horizontal, 14)
            .padding(.bottom, 10)
    }
}

// MARK: - Content view

struct ContentView: View {
    @StateObject private var viewModel = SessionsViewModel()

    var body: some View {
        ZStack {
            VisualEffectView()
                .ignoresSafeArea()

            VStack(spacing: 0) {
                // Header
                HStack {
                    Text("Claude")
                        .font(.system(size: 13, weight: .bold))
                        .foregroundColor(.primary)
                    Spacer()
                    Text("\(viewModel.sessions.count)")
                        .font(.system(size: 11, weight: .medium))
                        .foregroundColor(.secondary)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 2)
                        .background(
                            Capsule()
                                .fill(Color.primary.opacity(0.1))
                        )
                }
                .padding(.horizontal, 16)
                .padding(.top, 16)
                .padding(.bottom, 12)

                Divider()
                    .opacity(0.3)

                // Session list or empty state
                if viewModel.sessions.isEmpty {
                    EmptyState()
                        .frame(height: 140)
                } else if viewModel.sessions.count <= 7 {
                    // Natural height — no scroll, no wasted space
                    VStack(spacing: 6) {
                        ForEach(viewModel.sessions) { session in
                            SessionCard(session: session)
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                } else {
                    // Many sessions — scroll with thin bar
                    ScrollView(showsIndicators: true) {
                        LazyVStack(spacing: 6) {
                            ForEach(viewModel.sessions) { session in
                                SessionCard(session: session)
                            }
                        }
                        .padding(.horizontal, 12)
                        .padding(.vertical, 10)
                    }
                    .frame(maxHeight: 360)
                }

                // Usage card (only when balance data is available)
                if let usage = viewModel.usage, (usage.balance != nil || usage.providers != nil) {
                    UsageCard(usage: usage)
                }

                Divider()
                    .opacity(0.3)

                FooterView(
                    text: viewModel.timeSinceUpdate,
                    isStale: viewModel.isStale
                )
            }
        }
        .frame(width: 340)
        .fixedSize(horizontal: true, vertical: viewModel.sessions.count <= 7)
        .frame(maxHeight: viewModel.sessions.count > 7 ? 480 : nil)
        .onAppear {
            viewModel.startPolling()
        }
        .onDisappear {
            viewModel.stopPolling()
        }
        .onReceive(viewModel.$sessions) { _ in
            resizeWindow()
        }
        .onReceive(viewModel.$usage) { _ in
            resizeWindow()
        }
    }

    private func resizeWindow() {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) {
                guard let window = WindowDelegate.shared.window else { return }
                guard let hosting = window.contentView as? NSHostingView<AnyView> else {
                    // Fallback: use contentView bounds
                    let h = min(max(window.contentView?.bounds.height ?? 180, 180), 480)
                    var frame = window.frame
                    frame.origin.y += frame.height - h
                    frame.size.height = h
                    window.setFrame(frame, display: true, animate: true)
                    return
                }
                let ideal = hosting.intrinsicContentSize.height
                let h = min(max(ideal, 180), 480)
                var frame = window.frame
                frame.origin.y += frame.height - h
                frame.size.height = h
                window.setFrame(frame, display: true, animate: true)
            }
        }
    }

// MARK: - Color hex helper

extension Color {
    init(hex: String) {
        let hex = hex.trimmingCharacters(in: CharacterSet.alphanumerics.inverted)
        var int: UInt64 = 0
        Scanner(string: hex).scanHexInt64(&int)
        let r = Double((int >> 16) & 0xFF) / 255.0
        let g = Double((int >> 8) & 0xFF) / 255.0
        let b = Double(int & 0xFF) / 255.0
        self.init(red: r, green: g, blue: b)
    }
}

// MARK: - App delegate

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var window: NSWindow!

    func applicationDidFinishLaunching(_ notification: Notification) {
        let contentView = ContentView()

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 340, height: 180),
            styleMask: [.titled, .closable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.titlebarAppearsTransparent = true
        window.isOpaque = false
        window.backgroundColor = .clear
        window.level = .floating
        window.isMovableByWindowBackground = true
        window.isReleasedWhenClosed = false
        window.title = "Novascope"
        window.center()
        window.setFrameAutosaveName("NovascopeSessionsPanel")

        // Hide miniaturize and zoom buttons
        window.standardWindowButton(.miniaturizeButton)?.isHidden = true
        window.standardWindowButton(.zoomButton)?.isHidden = true

        window.contentView = NSHostingView(rootView: contentView)

        // Quit when window closes
        WindowDelegate.shared.window = window
        window.delegate = WindowDelegate.shared

        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
}

// MARK: - Window delegate

final class WindowDelegate: NSObject, NSWindowDelegate {
    static let shared = WindowDelegate()
    weak var window: NSWindow?

    func windowWillClose(_ notification: Notification) {
        NSApp.stop(nil)
    }
}

// MARK: - App entry

struct SessionsPanelApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate

    var body: some Scene {
        Settings { EmptyView() }
    }
}

// MARK: - main

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = AppDelegate()
app.delegate = delegate
_ = NSApplicationMain(CommandLine.argc, CommandLine.unsafeArgv)
