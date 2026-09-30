import SwiftUI
import UIKit

/// A small capsule on an Autopilot pick ("Rainy") that explains itself when tapped: why the pick
/// fits its day, and for the weather, where the forecast came from.
struct AutopilotBadgeButton: View {
    let badge: AutopilotBadge
    /// Drawn over a photo: a material background instead of the tint's wash, so it reads on
    /// any picture.
    var onPhoto = false
    /// Just the symbol, for tight spaces. VoiceOver still reads the label.
    var compact = false

    @State private var isShowingDetail = false

    var body: some View {
        Button {
            isShowingDetail = true
        } label: {
            HStack(spacing: 4) {
                if hasSymbol {
                    Image(systemName: badge.symbol)
                        .symbolRenderingMode(.multicolor)
                }
                if !compact || !hasSymbol {
                    Text(badge.label)
                }
            }
            .font(.caption.weight(.semibold))
            .lineLimit(1)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background {
                if onPhoto {
                    Capsule().fill(.regularMaterial)
                } else {
                    Capsule().fill(Color.accentColor.opacity(0.14))
                }
            }
            .foregroundStyle(onPhoto ? AnyShapeStyle(Color.primary) : AnyShapeStyle(.tint))
            // The capsule is small; the tap area isn't. Over a photo it grows its tap area
            // without growing its frame, so it lines up with the day label beside it.
            .frame(minHeight: onPhoto ? nil : 44)
            .contentShape(.rect.inset(by: onPhoto ? -10 : 0))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(badge.label)
        .accessibilityHint("Shows why this was picked")
        .popover(isPresented: $isShowingDetail) {
            AutopilotBadgeDetail(badge: badge)
                .presentationCompactAdaptation(.popover)
        }
    }
}

extension AutopilotBadgeButton {
    fileprivate var hasSymbol: Bool { !badge.symbol.isEmpty && UIImage(systemName: badge.symbol) != nil }
}

/// What a badge says when tapped.
struct AutopilotBadgeDetail: View {
    let badge: AutopilotBadge

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label {
                Text(badge.label).font(.headline)
            } icon: {
                if UIImage(systemName: badge.symbol) != nil {
                    Image(systemName: badge.symbol).symbolRenderingMode(.multicolor)
                }
            }
            Text(badge.detail)
                .font(.subheadline)
                .fixedSize(horizontal: false, vertical: true)
            if badge.isWeather {
                WeatherAttributionView()
            }
        }
        .padding()
        .frame(idealWidth: 300, maxWidth: 320, alignment: .leading)
    }
}

#Preview("Weather badge") {
    VStack(spacing: 20) {
        AutopilotBadgeButton(
            badge: AutopilotBadge(
                code: "weather", label: "Cold and rainy", symbol: "cloud.rain",
                detail: "Picked for the weather: Tuesday looks cold and rainy, so a warm, comforting dinner."))
        AutopilotBadgeDetail(
            badge: AutopilotBadge(
                code: "weather", label: "Hot", symbol: "sun.max",
                detail: "Picked for the weather: Wednesday looks hot, so something lighter."))
    }
}
