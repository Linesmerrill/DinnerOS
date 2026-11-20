import SwiftUI

/// "Continue with Google", following Google's sign-in branding: the full-color "G" on Google's
/// neutral button (light: white fill, gray outline; dark: near-black fill, light outline), in the
/// pill shape Google offers. Its size, title and icon slot match the other sign-in buttons
/// (`SignInButtonLabel`, build 120).
struct GoogleSignInButton: View {
    let action: () -> Void

    @ScaledMetric(relativeTo: .body) private var height: CGFloat = DS.ButtonSize.large.height

    var body: some View {
        Button(action: action) {
            SignInButtonLabel(title: Text("Continue with Google"), height: height) {
                // Google's mark, unaltered, from Google's published artwork (a vector asset).
                Image("GoogleG")
                    .resizable()
                    .scaledToFit()
            }
        }
        .buttonStyle(
            DSOutlineButtonStyle(
                size: .large, fullWidth: true, font: DS.Typography.signInButton(height: height),
                palette: { $0 == .dark ? Self.dark : Self.light })
        )
        .accessibilityIdentifier("signIn.google")
    }

    private static let light = DSOutlineButtonStyle.Palette(
        fill: .white,
        stroke: Color(red: 0x74 / 255, green: 0x77 / 255, blue: 0x75 / 255),
        text: Color(red: 0x1F / 255, green: 0x1F / 255, blue: 0x1F / 255))
    private static let dark = DSOutlineButtonStyle.Palette(
        fill: Color(red: 0x13 / 255, green: 0x13 / 255, blue: 0x14 / 255),
        stroke: Color(red: 0x8E / 255, green: 0x91 / 255, blue: 0x8F / 255),
        text: Color(red: 0xE3 / 255, green: 0xE3 / 255, blue: 0xE3 / 255))
}

/// A sign-in button's content: an icon in a fixed square slot, then the title, centered as Sign
/// in with Apple's button centers its logo and title.
struct SignInButtonLabel<Icon: View>: View {
    let title: Text
    let height: CGFloat
    @ViewBuilder let icon: () -> Icon

    var body: some View {
        HStack(spacing: (height * 0.12).rounded()) {
            icon()
                .frame(width: Self.iconSize(height), height: Self.iconSize(height))
                .accessibilityHidden(true)
            title
        }
    }

    /// The icon slot: 40% of the button height (20 pt at 50).
    static func iconSize(_ height: CGFloat) -> CGFloat { (height * 0.4).rounded() }
}

#Preview {
    VStack {
        GoogleSignInButton {}
        GoogleSignInButton {}.environment(\.colorScheme, .dark)
    }
    .padding()
}
