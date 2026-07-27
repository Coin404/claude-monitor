import Cocoa
import SwiftUI

// novascope-settings-helper — standalone SwiftUI helper that shows the
// DeepSeek API Key management settings window. Reads /tmp/claude-monitor-keys.json
// and writes user actions to /tmp/claude-monitor-key-actions.json.
//
// Layout: left tab column (API Keys / Config) + right content area.

// MARK: - Data model

struct KeyDisplayInfo: Codable, Equatable, Identifiable {
    var id: String { _id }
    let _id: String
    let label: String
    let maskedKey: String
    let active: Bool
    let balance: Double
    let todaySpending: Double
    let currency: String
    let error: String
    let createdAt: String
    let model: String

    enum CodingKeys: String, CodingKey {
        case _id = "id"
        case label, maskedKey, active, balance, todaySpending, currency, error, createdAt, model
    }
}

struct KeysSnapshot: Codable {
    let keys: [KeyDisplayInfo]
    let count: Int
    let timestamp: String
    let refreshIntervalSec: Int
    let pollIntervalMs: Int
    let monthlySalary: Double
}

struct KeyAction: Codable {
    let action: String
    let id: String?
    let label: String?
    let key: String?
}

// MARK: - ViewModel

final class SettingsViewModel: ObservableObject {
    @Published var keys: [KeyDisplayInfo] = []
    @Published var lastUpdate: Date?
    @Published var refreshIntervalSec: Int = 30
    @Published var pollIntervalMs: Int = 10
    @Published var monthlySalary: Double = 0

    private var timer: Timer?
    private var hasLoadedSettings = false
    private let jsonPath = "/tmp/claude-monitor-keys.json"
    private let actionPath = "/tmp/claude-monitor-key-actions.json"

    func startPolling() {
        refresh()
        timer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { [weak self] _ in
            self?.refresh()
        }
    }

    func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func refresh() {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: jsonPath)) else { return }
        guard let snap = try? JSONDecoder().decode(KeysSnapshot.self, from: data) else { return }
        if snap.keys != keys {
            keys = snap.keys
        }
        if !hasLoadedSettings {
            refreshIntervalSec = snap.refreshIntervalSec
            pollIntervalMs = snap.pollIntervalMs
            monthlySalary = snap.monthlySalary
            hasLoadedSettings = true
        }
        lastUpdate = Date()
    }

    func sendAction(_ action: KeyAction) {
        guard let data = try? JSONEncoder().encode(action) else { return }
        try? data.write(to: URL(fileURLWithPath: actionPath), options: .atomic)
    }

    func addKey(label: String, key: String) {
        sendAction(KeyAction(action: "add", id: nil, label: label, key: key))
    }

    func deleteKey(id: String) {
        sendAction(KeyAction(action: "delete", id: id, label: nil, key: nil))
    }

    func toggleKey(id: String) {
        sendAction(KeyAction(action: "toggle", id: id, label: nil, key: nil))
    }

    func editKey(id: String, label: String) {
        sendAction(KeyAction(action: "edit", id: id, label: label, key: nil))
    }

    func refreshBalances() {
        sendAction(KeyAction(action: "refresh", id: nil, label: nil, key: nil))
    }

    func setInterval(seconds: Int) {
        sendAction(KeyAction(action: "setInterval", id: nil, label: nil, key: String(seconds)))
    }

    func setPollInterval(ms: Int) {
        sendAction(KeyAction(action: "setPollInterval", id: nil, label: nil, key: String(ms)))
    }

    func setSalary(_ salary: Double) {
        monthlySalary = salary
        sendAction(KeyAction(action: "setSalary", id: nil, label: nil, key: String(salary)))
    }

    var activeKey: KeyDisplayInfo? {
        keys.first(where: { $0.active })
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

// MARK: - Key row

struct KeyRow: View {
    let keyInfo: KeyDisplayInfo
    let onToggle: () -> Void
    let onDelete: () -> Void
    let onEdit: (String) -> Void
    @State private var isHovered = false
    @State private var showEditSheet = false
    @State private var editLabel = ""

    var body: some View {
        HStack(spacing: 10) {
            // Status dot — green when active, gray otherwise
            Circle()
                .fill(keyInfo.active ? Color.green : Color.gray.opacity(0.4))
                .frame(width: 8, height: 8)
                .shadow(color: keyInfo.active ? Color.green.opacity(0.5) : Color.clear, radius: 3)

            VStack(alignment: .leading, spacing: 2) {
                Text(keyInfo.label)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(.primary)
                    .lineLimit(1)
                Text(keyInfo.maskedKey)
                    .font(.system(size: 11, weight: .regular, design: .monospaced))
                    .foregroundColor(.secondary)
                    .lineLimit(1)
                if !keyInfo.model.isEmpty {
                    Text(keyInfo.model)
                        .font(.system(size: 10, weight: .regular, design: .monospaced))
                        .foregroundColor(.secondary.opacity(0.7))
                        .lineLimit(1)
                }
            }

            Spacer()

            // Balance info
            VStack(alignment: .trailing, spacing: 2) {
                if !keyInfo.error.isEmpty {
                    Text(keyInfo.error)
                        .font(.system(size: 10))
                        .foregroundColor(.orange)
                } else if keyInfo.balance > 0 || keyInfo.todaySpending > 0 {
                    Text("\(keyInfo.currency) \(String(format: "%.2f", keyInfo.balance))")
                        .font(.system(size: 11, weight: .medium))
                        .foregroundColor(.primary)
                        .monospacedDigit()
                    if keyInfo.todaySpending > 0 {
                        Text("-\(keyInfo.currency) \(String(format: "%.2f", keyInfo.todaySpending))")
                            .font(.system(size: 10))
                            .foregroundColor(.secondary)
                            .monospacedDigit()
                    }
                }
            }

            // Action buttons (hover)
            if isHovered {
                HStack(spacing: 4) {
                    Button(action: {
                        editLabel = keyInfo.label
                        showEditSheet = true
                    }) {
                        Image(systemName: "pencil")
                            .font(.system(size: 10, weight: .medium))
                            .foregroundColor(.secondary)
                    }
                    .buttonStyle(.plain)
                    .help("Edit label")

                    Button(action: onDelete) {
                        Image(systemName: "trash")
                            .font(.system(size: 10, weight: .medium))
                            .foregroundColor(.red)
                    }
                    .buttonStyle(.plain)
                    .help("Delete key")
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(isHovered
                    ? Color.primary.opacity(0.06)
                    : Color.primary.opacity(0.03))
        )
        .onHover { hovering in
            isHovered = hovering
        }
        .onTapGesture {
            onToggle()
        }
        .sheet(isPresented: $showEditSheet) {
            EditSheetView(
                label: $editLabel,
                onSave: {
                    onEdit(editLabel)
                    showEditSheet = false
                },
                onCancel: { showEditSheet = false }
            )
        }
    }
}

// MARK: - Add key bottom sheet

struct AddKeySheet: View {
    @Binding var label: String
    @Binding var apiKey: String
    let onAdd: () -> Void
    let onCancel: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            Text("Add DeepSeek API Key")
                .font(.system(size: 15, weight: .semibold))
                .foregroundColor(.primary)

            VStack(alignment: .leading, spacing: 6) {
                Text("Label")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(.secondary)
                TextField("e.g. Personal Key", text: $label)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13))
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.primary.opacity(0.06))
                    )
            }

            VStack(alignment: .leading, spacing: 6) {
                Text("API Key")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(.secondary)
                SecureField("sk-...", text: $apiKey)
                    .textFieldStyle(.plain)
                    .font(.system(size: 13, design: .monospaced))
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.primary.opacity(0.06))
                    )
            }

            HStack(spacing: 12) {
                Button("Cancel", action: onCancel)
                    .buttonStyle(.plain)
                    .font(.system(size: 13))
                    .foregroundColor(.secondary)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.primary.opacity(0.08))
                    )

                Button("Add Key", action: onAdd)
                    .buttonStyle(.plain)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(.white)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(canAdd ? Color.blue : Color.gray.opacity(0.3))
                    )
                    .disabled(!canAdd)
            }
        }
        .padding(20)
        .frame(width: 320)
    }

    private var canAdd: Bool {
        !apiKey.trimmingCharacters(in: .whitespaces).isEmpty
    }
}

// MARK: - Edit label sheet

struct EditSheetView: View {
    @Binding var label: String
    let onSave: () -> Void
    let onCancel: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            Text("Edit Label")
                .font(.system(size: 15, weight: .semibold))
                .foregroundColor(.primary)

            TextField("Label", text: $label)
                .textFieldStyle(.plain)
                .font(.system(size: 13))
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(Color.primary.opacity(0.06))
                )

            HStack(spacing: 12) {
                Button("Cancel", action: onCancel)
                    .buttonStyle(.plain)
                    .font(.system(size: 13))
                    .foregroundColor(.secondary)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.primary.opacity(0.08))
                    )

                Button("Save", action: onSave)
                    .buttonStyle(.plain)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(.white)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.blue)
                    )
            }
        }
        .padding(20)
        .frame(width: 280)
    }
}

// MARK: - Empty state

struct EmptyKeysView: View {
    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "key")
                .font(.system(size: 36))
                .foregroundColor(.secondary.opacity(0.5))
            Text("No API Keys Configured")
                .font(.system(size: 14, weight: .semibold))
                .foregroundColor(.secondary)
            Text("Add a DeepSeek API key to track balance")
                .font(.system(size: 12))
                .foregroundColor(.secondary.opacity(0.7))
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 40)
    }
}

// MARK: - Active key card (right column)

struct ActiveKeyCard: View {
    let key: KeyDisplayInfo?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Active Key")
                .font(.system(size: 11, weight: .semibold))
                .foregroundColor(.secondary)

            if let key = key {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        Circle()
                            .fill(Color.green)
                            .frame(width: 6, height: 6)
                        Text(key.label)
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundColor(.primary)
                            .lineLimit(1)
                    }
                    if !key.model.isEmpty {
                        Text(key.model)
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundColor(.secondary)
                            .lineLimit(1)
                    }
                    if !key.error.isEmpty {
                        Text(key.error)
                            .font(.system(size: 10))
                            .foregroundColor(.orange)
                    } else if key.balance > 0 {
                        Text("\(key.currency) \(String(format: "%.2f", key.balance))")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundColor(.primary)
                            .monospacedDigit()
                        if key.todaySpending > 0 {
                            Text("Today: -\(key.currency) \(String(format: "%.2f", key.todaySpending))")
                                .font(.system(size: 10))
                                .foregroundColor(.secondary)
                                .monospacedDigit()
                        }
                    }
                }
            } else {
                HStack(spacing: 6) {
                    Circle()
                        .fill(Color.gray.opacity(0.4))
                        .frame(width: 6, height: 6)
                    Text("No active key")
                        .font(.system(size: 12))
                        .foregroundColor(.secondary)
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color.primary.opacity(0.04))
        )
    }
}

// MARK: - Content view (tab column + content area)

struct ContentView: View {
    @StateObject private var viewModel = SettingsViewModel()
    @State private var selectedTab = "keys"
    @State private var showAddSheet = false
    @State private var newLabel = ""
    @State private var newKey = ""
    @State private var skinMode: String = loadSkinMode()

    var body: some View {
        ZStack {
            if skinMode == "white" {
                Color(NSColor.windowBackgroundColor)
                    .ignoresSafeArea()
            } else {
                VisualEffectView()
                    .ignoresSafeArea()
            }

            HStack(spacing: 0) {
                // MARK: Left tab column
                tabColumn

                // Vertical divider
                Rectangle()
                    .fill(Color.primary.opacity(0.1))
                    .frame(width: 1)

                // MARK: Right content area
                if selectedTab == "keys" {
                    keysContent
                } else {
                    configContent
                }
            }
        }
        .frame(width: 560, height: 420)
        .onAppear {
            viewModel.startPolling()
        }
        .onDisappear {
            viewModel.stopPolling()
        }
        .sheet(isPresented: $showAddSheet) {
            AddKeySheet(
                label: $newLabel,
                apiKey: $newKey,
                onAdd: {
                    viewModel.addKey(label: newLabel, key: newKey)
                    newLabel = ""
                    newKey = ""
                    showAddSheet = false
                },
                onCancel: {
                    newLabel = ""
                    newKey = ""
                    showAddSheet = false
                }
            )
        }
    }

    // MARK: - Tab column

    var tabColumn: some View {
        VStack(spacing: 0) {
            // Navigation tabs
            VStack(spacing: 2) {
                TabButton(
                    icon: "key",
                    label: "API Keys",
                    isSelected: selectedTab == "keys",
                    action: { selectedTab = "keys" }
                )
                TabButton(
                    icon: "gearshape",
                    label: "Config",
                    isSelected: selectedTab == "config",
                    action: { selectedTab = "config" }
                )
            }
            .padding(.horizontal, 8)
            .padding(.top, 16)

            Spacer()

            // Version info at bottom
            Text("Novascope")
                .font(.system(size: 9, weight: .medium))
                .foregroundColor(.secondary.opacity(0.5))
                .padding(.bottom, 12)
        }
        .frame(width: 140)
    }

    // MARK: - API Keys content

    var keysContent: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("API Keys")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundColor(.primary)
                Text("\(viewModel.keys.count)")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(.secondary)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 2)
                    .background(
                        Capsule()
                            .fill(Color.primary.opacity(0.1))
                    )
                Spacer()
                Button(action: { showAddSheet = true }) {
                    HStack(spacing: 4) {
                        Image(systemName: "plus")
                            .font(.system(size: 10, weight: .medium))
                        Text("Add Key")
                            .font(.system(size: 11, weight: .medium))
                    }
                    .foregroundColor(.blue)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(Color.blue.opacity(0.1))
                    )
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 16)
            .padding(.top, 16)
            .padding(.bottom, 12)

            Divider()
                .opacity(0.3)

            // Key list or empty state
            if viewModel.keys.isEmpty {
                EmptyKeysView()
            } else {
                ScrollView(showsIndicators: true) {
                    LazyVStack(spacing: 4) {
                        ForEach(viewModel.keys) { key in
                            KeyRow(
                                keyInfo: key,
                                onToggle: { viewModel.toggleKey(id: key.id) },
                                onDelete: { viewModel.deleteKey(id: key.id) },
                                onEdit: { newLabel in viewModel.editKey(id: key.id, label: newLabel) }
                            )
                        }
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                }
            }
        }
    }

    // MARK: - Config content

    var configContent: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Config")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundColor(.primary)
                Spacer()
            }
            .padding(.horizontal, 16)
            .padding(.top, 16)
            .padding(.bottom, 12)

            Divider()
                .opacity(0.3)

            VStack(spacing: 0) {
                // Poll interval
                ConfigRow(label: "Poll Interval") {
                    Picker("", selection: Binding<Int>(
                        get: { viewModel.pollIntervalMs },
                        set: { viewModel.setPollInterval(ms: $0) }
                    )) {
                        Text("5ms").tag(5)
                        Text("10ms").tag(10)
                        Text("30ms").tag(30)
                        Text("50ms").tag(50)
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .frame(width: 100, alignment: .trailing)
                }

                Divider()
                    .opacity(0.2)
                    .padding(.leading, 16)

                // Balance refresh
                ConfigRow(label: "Balance Refresh") {
                    Picker("", selection: Binding<Int>(
                        get: { viewModel.refreshIntervalSec },
                        set: { viewModel.setInterval(seconds: $0) }
                    )) {
                        Text("1s").tag(1)
                        Text("10s").tag(10)
                        Text("30s").tag(30)
                        Text("60s").tag(60)
                        Text("5m").tag(300)
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .frame(width: 100, alignment: .trailing)
                }

                Divider()
                    .opacity(0.2)
                    .padding(.leading, 16)

                // Appearance
                ConfigRow(label: "Appearance") {
                    Picker("", selection: Binding<String>(
                        get: { skinMode },
                        set: {
                            skinMode = $0
                            saveSkinMode($0)
                        }
                    )) {
                        Text("Glass").tag("glass")
                        Text("White").tag("white")
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .frame(width: 140)
                }

                Divider()
                    .opacity(0.2)
                    .padding(.leading, 16)

                // Monthly salary
                ConfigRow(label: "Monthly Salary") {
                    HStack(spacing: 4) {
                        Text("CNY")
                            .font(.system(size: 12))
                            .foregroundColor(.secondary)
                        TextField("0", value: $viewModel.monthlySalary, format: .number)
                            .textFieldStyle(.plain)
                            .font(.system(size: 13, design: .monospaced))
                            .multilineTextAlignment(.trailing)
                            .frame(width: 100)
                            .onSubmit {
                                viewModel.setSalary(viewModel.monthlySalary)
                            }
                    }
                }
            }

            Spacer()
        }
    }
}

// MARK: - Config row

struct ConfigRow<Content: View>: View {
    let label: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        HStack {
            Text(label)
                .font(.system(size: 12, weight: .medium))
                .foregroundColor(.primary)
            Spacer()
            content()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }
}

// MARK: - Tab button

struct TabButton: View {
    let icon: String
    let label: String
    let isSelected: Bool
    let action: () -> Void
    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                Image(systemName: icon)
                    .font(.system(size: 12, weight: .medium))
                    .frame(width: 16)
                Text(label)
                    .font(.system(size: 12, weight: .medium))
                Spacer()
            }
            .foregroundColor(isSelected ? .blue : .secondary)
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 6)
                    .fill(backgroundColor)
            )
            .overlay(
                // Blue left border when selected
                RoundedRectangle(cornerRadius: 6)
                    .stroke(
                        isSelected ? Color.blue.opacity(0.6) : Color.clear,
                        lineWidth: isSelected ? 1.5 : 0
                    )
            )
        }
        .buttonStyle(.plain)
        .onHover { hovering in
            isHovered = hovering
        }
    }

    private var backgroundColor: Color {
        if isSelected {
            return Color.blue.opacity(0.1)
        }
        if isHovered {
            return Color.primary.opacity(0.06)
        }
        return Color.clear
    }
}

// MARK: - App delegate

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var window: NSWindow!

    func applicationDidFinishLaunching(_ notification: Notification) {
        let contentView = ContentView()

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 560, height: 420),
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
        window.title = "Novascope Settings"
        window.center()
        window.setFrameAutosaveName("NovascopeSettingsWindow")

        window.standardWindowButton(.miniaturizeButton)?.isHidden = true
        window.standardWindowButton(.zoomButton)?.isHidden = true

        window.contentView = NSHostingView(rootView: contentView)

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

// MARK: - Skin persistence (shared via /tmp with sessions panel)

private let skinPath = "/tmp/claude-monitor-skin.json"

private func loadSkinMode() -> String {
    guard let data = try? Data(contentsOf: URL(fileURLWithPath: skinPath)),
          let obj = try? JSONDecoder().decode([String: String].self, from: data),
          let mode = obj["skinMode"] else { return "glass" }
    return mode
}

private func saveSkinMode(_ mode: String) {
    let obj = ["skinMode": mode]
    guard let data = try? JSONEncoder().encode(obj) else { return }
    try? data.write(to: URL(fileURLWithPath: skinPath), options: .atomic)
}

// MARK: - App entry

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = AppDelegate()
app.delegate = delegate
_ = NSApplicationMain(CommandLine.argc, CommandLine.unsafeArgv)
