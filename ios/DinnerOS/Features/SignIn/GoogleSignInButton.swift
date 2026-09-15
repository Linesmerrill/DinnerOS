import SwiftUI

/// "Continue with Google", following Google's neutral button palette (light: white
/// fill, gray outline; dark: near-black fill, light outline) without bundling the logo.
struct GoogleSignInButton: View {
    let action: () -> Void

    @ScaledMetric(relativeTo: .body) private var minimumHeight: CGFloat = 50

    var body: some View {
        Button(action: action) {
            Text("Continue with Google")
                .font(.body.weight(.medium))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity, minHeight: minimumHeight)
        }
        .buttonStyle(GoogleButtonStyle())
    }
}

private struct GoogleButtonStyle: ButtonStyle {
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.isEnabled) private var isEnabled

    private struct Palette {
        let fill: Color
        let stroke: Color
        let text: Color
    }

    private static let light = Palette(
        fill: .white,
        stroke: Color(red: 0x74 / 255, green: 0x77 / 255, blue: 0x75 / 255),
        text: Color(red: 0x1F / 255, green: 0x1F / 255, blue: 0x1F / 255))
    private static let dark = Palette(
        fill: Color(red: 0x13 / 255, green: 0x13 / 255, blue: 0x14 / 255),
        stroke: Color(red: 0x8E / 255, green: 0x91 / 255, blue: 0x8F / 255),
        text: Color(red: 0xE3 / 255, green: 0xE3 / 255, blue: 0xE3 / 255))

    func makeBody(configuration: Configuration) -> some View {
        let palette = colorScheme == .dark ? Self.dark : Self.light
        let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)
        configuration.label
            .padding(.horizontal, 12)
            .foregroundStyle(palette.text)
            .background(palette.fill, in: shape)
            .overlay(shape.strokeBorder(palette.stroke, lineWidth: 1))
            .contentShape(shape)
            .opacity(isEnabled ? (configuration.isPressed ? 0.8 : 1) : 0.5)
    }
}

#Preview {
    VStack {
        GoogleSignInButton {}
        GoogleSignInButton {}.environment(\.colorScheme, .dark)
    }
    .padding()
}
