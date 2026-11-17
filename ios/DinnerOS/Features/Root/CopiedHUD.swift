import SwiftUI
import UIKit

/// Everything the app puts on the clipboard goes through here, so every copy says "Copied" the
/// same way: a centered HUD with a check mark, a success haptic, and a VoiceOver announcement.
enum Clipboard {
    /// Copies `text` and shows the Copied HUD. `expiresAfter` takes it off the clipboard later,
    /// and keeps it off the person's other devices, for codes that shouldn't linger there.
    @MainActor
    static func copy(_ text: String, expiresAfter: TimeInterval? = nil) {
        if let expiresAfter {
            UIPasteboard.general.setItems(
                [["public.plain-text": text]],
                options: [.expirationDate: Date.now.addingTimeInterval(expiresAfter), .localOnly: true])
        } else {
            UIPasteboard.general.string = text
        }
        CopiedHUD.shared.show()
    }
}

/// When the HUD last showed. One for the app, so a copy anywhere shows it above everything,
/// sheets included.
@MainActor
@Observable
final class CopiedHUD {
    static let shared = CopiedHUD()
    /// How long the HUD stays before fading. UI tests can lengthen it in a debug build: XCTest
    /// sometimes looks more than 1.5 seconds after a tap.
    static let duration: Duration = {
        #if DEBUG
            if let seconds = ProcessInfo.processInfo.environment["QA_HUD_SECONDS"].flatMap(Double.init) {
                return .seconds(seconds)
            }
        #endif
        return .seconds(1.5)
    }()

    /// Counts copies, so copying twice in a row shows it again.
    private(set) var copies = 0

    func show() {
        copies += 1
        AccessibilityNotification.Announcement(String(localized: "Copied")).post()
    }
}

extension View {
    /// Shows the Copied HUD in a window of its own over the app, so it appears above sheets.
    /// Attach once, at the app's root.
    func copiedHUD() -> some View {
        background(CopiedHUDInstaller())
    }
}

/// The HUD itself: a large check mark over "Copied" on a blurred rounded square.
struct CopiedHUDView: View {
    @State private var visible = false
    @State private var hide: Task<Void, Never>?
    private let hud = CopiedHUD.shared

    var body: some View {
        ZStack {
            if visible {
                VStack(spacing: 10) {
                    Image(systemName: "checkmark")
                        .font(.system(size: 44, weight: .semibold))
                    Text("Copied")
                        .font(.headline)
                }
                .foregroundStyle(.primary)
                .frame(width: 150, height: 150)
                .background(.regularMaterial, in: .rect(cornerRadius: 22))
                .transition(.opacity)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Copied")
                .accessibilityIdentifier("CopiedHUD")
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .allowsHitTesting(false)
        .sensoryFeedback(.success, trigger: hud.copies)
        .onChange(of: hud.copies) { _, _ in
            withAnimation(.easeOut(duration: 0.2)) { visible = true }
            hide?.cancel()
            hide = Task {
                try? await Task.sleep(for: CopiedHUD.duration)
                guard !Task.isCancelled else { return }
                withAnimation(.easeIn(duration: 0.3)) { visible = false }
            }
        }
    }
}

/// Puts `CopiedHUDView` in a pass-through window above the app's own, once per scene.
private struct CopiedHUDInstaller: UIViewRepresentable {
    func makeUIView(context: Context) -> InstallerView { InstallerView() }
    func updateUIView(_ uiView: InstallerView, context: Context) {}

    final class InstallerView: UIView {
        private var hudWindow: UIWindow?

        override func didMoveToWindow() {
            super.didMoveToWindow()
            guard hudWindow == nil, let scene = window?.windowScene else { return }
            let window = UIWindow(windowScene: scene)
            window.windowLevel = .alert + 1
            // Never takes a touch: everything goes to the app underneath. Turning interaction
            // off, rather than refusing hit tests, keeps the HUD visible to VoiceOver and tests.
            window.isUserInteractionEnabled = false
            let host = UIHostingController(rootView: CopiedHUDView())
            host.view.backgroundColor = .clear
            window.rootViewController = host
            window.isHidden = false
            hudWindow = window
        }
    }
}
