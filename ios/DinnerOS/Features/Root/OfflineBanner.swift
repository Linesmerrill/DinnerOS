import SwiftUI

/// A small line at the top while the app is showing saved answers: "Offline · Saved 3:42 PM".
/// It never blocks anything; everything saved still works.
struct OfflineBanner: View {
    /// Optional so previews and tests needn't supply one.
    @Environment(OfflineStatus.self) private var offline: OfflineStatus?

    var body: some View {
        if let offline, offline.isShowingSaved {
            HStack(spacing: 6) {
                Image(systemName: "wifi.slash")
                    .font(.caption.weight(.bold))
                    .accessibilityHidden(true)
                Text(text(offline))
                    .font(.caption.weight(.semibold))
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(.regularMaterial, in: Capsule())
            .overlay(Capsule().strokeBorder(Color.primary.opacity(0.08)))
            .shadow(color: .black.opacity(0.12), radius: 8, y: 2)
            .transition(.move(edge: .top).combined(with: .opacity))
            .allowsHitTesting(false)
            .accessibilityElement(children: .combine)
        }
    }

    private func text(_ offline: OfflineStatus) -> String {
        guard let saved = offline.savedAt else { return String(localized: "Offline. Showing what's saved.") }
        let when =
            Calendar.current.isDateInToday(saved)
            ? saved.formatted(date: .omitted, time: .shortened)
            : saved.formatted(.dateTime.month(.abbreviated).day().hour().minute())
        return String(localized: "Offline · Saved \(when)")
    }
}

extension View {
    /// The Offline line at the top of the screen. It takes its own space, so it never covers a
    /// toolbar, and none at all while online.
    func offlineBanner() -> some View {
        safeAreaInset(edge: .top, spacing: 0) {
            OfflineBanner()
                .padding(.bottom, 4)
        }
    }
}
