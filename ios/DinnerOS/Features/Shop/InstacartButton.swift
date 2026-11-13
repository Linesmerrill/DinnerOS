import SwiftUI
import UIKit

/// Instacart's own call-to-action button, built to its CTA design rules
/// (docs.instacart.com/developer_platform_api/guide/concepts/design/cta_design): a round
/// button 46 points tall with 18 points of side padding, Instacart's logo at 22 points, and the
/// dark scheme (#003D29 with #FAF1E5 text). The words are one of the two Instacart allows.
struct InstacartButton: View {
    /// Instacart's approved button words.
    enum Title {
        case shopOnInstacart
        case shopIngredients

        var text: LocalizedStringKey {
            switch self {
            case .shopOnInstacart: "Shop on Instacart"
            case .shopIngredients: "Shop ingredients"
            }
        }
    }

    var title: Title = .shopOnInstacart
    var isWorking = false
    let action: () -> Void

    /// Instacart's dark scheme.
    static let background = Color(red: 0x00 / 255, green: 0x3D / 255, blue: 0x29 / 255)
    static let foreground = Color(red: 0xFA / 255, green: 0xF1 / 255, blue: 0xE5 / 255)
    /// The logo from Instacart's logo pack, once it's in the asset catalog.
    static let logoName = "InstacartLogo"

    @ScaledMetric(relativeTo: .body) private var height: CGFloat = 46
    @ScaledMetric(relativeTo: .body) private var logoSize: CGFloat = 22

    var body: some View {
        Button(action: action) {
            HStack(spacing: 8) {
                if isWorking {
                    ProgressView()
                        .tint(Self.foreground)
                } else {
                    if UIImage(named: Self.logoName) != nil {
                        Image(Self.logoName)
                            .resizable()
                            .scaledToFit()
                            .frame(width: logoSize, height: logoSize)
                            .accessibilityHidden(true)
                    }
                    Text(title.text)
                        .font(.body.weight(.semibold))
                }
            }
            .foregroundStyle(Self.foreground)
            .padding(.horizontal, 18)
            // 46 points tall at the default text size, taller with larger text.
            .padding(.vertical, 4)
            .frame(minHeight: height)
            .frame(maxWidth: .infinity)
            .background(Self.background, in: .capsule)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text(title.text))
    }
}

#Preview {
    VStack(spacing: 16) {
        InstacartButton {}
        InstacartButton(title: .shopIngredients) {}
        InstacartButton(isWorking: true) {}
    }
    .padding()
}
