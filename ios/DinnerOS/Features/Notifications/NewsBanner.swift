import SwiftUI

/// A notification shown across the top when the app opens, for news whose push may have gone
/// unseen ("Your free month is ready."). Tapping opens it; the X puts it away. Either way
/// it's marked read and doesn't come back.
struct NewsBanner: View {
    let notification: AppNotification
    let open: () -> Void
    let close: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: DS.Space.m) {
            Button(action: open) {
                HStack(alignment: .top, spacing: DS.Space.m) {
                    Image(systemName: icon)
                        .font(.title3)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: DS.Space.hairline) {
                        Text(notification.title)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.primary)
                        Text(notification.body)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    Spacer(minLength: 0)
                }
                .multilineTextAlignment(.leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint(NotificationRouting.destination(for: notification).accessibilityHint)
            Button("Close", systemImage: "xmark", action: close)
                .labelStyle(.iconOnly)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(.rect)
        }
        .padding(.leading, DS.Space.ml)
        .padding(.vertical, DS.Space.xs)
        .background(.regularMaterial, in: .rect(cornerRadius: DS.Radius.large))
        .shadow(color: .black.opacity(0.12), radius: 10, y: 4)
        .frame(maxWidth: 560)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("NewsBanner")
    }

    private var icon: String {
        notification.type == .plusReferral ? "gift.fill" : "bell.fill"
    }
}
