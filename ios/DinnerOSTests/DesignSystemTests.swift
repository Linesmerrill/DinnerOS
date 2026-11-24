import SwiftUI
import Testing
import UIKit

@testable import DinnerOS

/// The shared button styles land on their token heights (build 120): the sign-in screen's Face ID
/// button once came out 80 pt beside Apple's 50.
@MainActor
struct DesignSystemTests {
    private func size<V: View>(_ view: V, _ dynamicType: DynamicTypeSize = .large) -> CGSize {
        let host = UIHostingController(rootView: view.environment(\.dynamicTypeSize, dynamicType))
        return host.sizeThatFits(in: CGSize(width: 390, height: 1000))
    }

    private func button(_ title: String = "Add to Week") -> Button<Text> {
        Button(title) {}
    }

    @Test func largeButtonsAreFiftyPoints() {
        for height in [
            size(button().buttonStyle(.dsPrimary)).height,
            size(button().buttonStyle(.dsSecondary)).height,
            size(button().buttonStyle(.dsOutline)).height,
            size(button().buttonStyle(.dsPrimary(fullWidth: true))).height,
        ] {
            #expect(abs(height - 50) < 1, "a large button is \(height) pt")
        }
    }

    @Test func smallButtonsAreThirtyFourPoints() {
        for height in [
            size(button("Add").buttonStyle(.dsPrimary(.small))).height,
            size(button("Edit").buttonStyle(.dsSecondary(.small))).height,
            size(button("Skip").buttonStyle(.dsOutline(.small))).height,
        ] {
            #expect(abs(height - 34) < 1, "a small button is \(height) pt")
        }
    }

    @Test func aLabelWithAnIconKeepsTheHeight() {
        let withIcon = Button {
        } label: {
            Label("Start Cooking", systemImage: "frying.pan")
        }
        #expect(abs(size(withIcon.buttonStyle(.dsSecondary)).height - 50) < 1)
    }

    @Test func fullWidthFillsTheRow() {
        #expect(abs(size(button().buttonStyle(.dsPrimary(fullWidth: true))).width - 390) < 1)
        #expect(size(button().buttonStyle(.dsPrimary)).width < 300)
    }

    @Test func textLinksAreAtLeastATapTargetTall() {
        #expect(size(button("Sign in with email").buttonStyle(.dsTextLink)).height >= DS.Size.tapTarget)
    }

    @Test func largerTextMakesTallerButtons() {
        let normal = size(button().buttonStyle(.dsPrimary)).height
        let larger = size(button().buttonStyle(.dsPrimary), .xxxLarge).height
        #expect(larger > normal)
        // Accessibility sizes still fit the title.
        let huge = size(button().buttonStyle(.dsPrimary), .accessibility3).height
        #expect(huge >= larger)
    }

    /// WCAG contrast ratio of two colors resolved for a light or dark trait.
    private func contrast(_ a: Color, _ b: Color, dark: Bool) -> CGFloat {
        let traits = UITraitCollection(userInterfaceStyle: dark ? .dark : .light)
        func luminance(_ color: Color) -> CGFloat {
            var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, alpha: CGFloat = 0
            UIColor(color).resolvedColor(with: traits).getRed(&r, green: &g, blue: &b, alpha: &alpha)
            func channel(_ c: CGFloat) -> CGFloat { c <= 0.03928 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4) }
            return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
        }
        let (l1, l2) = (luminance(a), luminance(b))
        return (max(l1, l2) + 0.05) / (min(l1, l2) + 0.05)
    }

    /// Hark's build 120 review: white on dark mode's light green was about 2.2:1.
    @Test func textOnTheAccentIsReadableInLightAndDark() {
        let accent = Color("AccentColor")
        #expect(contrast(DS.Colors.onAccent, accent, dark: false) >= 4.5)
        #expect(contrast(DS.Colors.onAccent, accent, dark: true) >= 4.5)
    }

    /// Hark's build 121 review: an empty rating star was hard to see in dark mode. As an icon it
    /// needs 3:1 on the screen and on a card, in light and dark.
    @Test func emptyRatingStarsAreClearInLightAndDark() {
        for dark in [false, true] {
            let backgrounds = [
                DS.Colors.background, DS.Colors.surface, DS.Colors.groupedSurface, DS.Colors.groupedBackground,
            ]
            for background in backgrounds {
                #expect(contrast(DS.Colors.emptyStar, background, dark: dark) >= 3)
            }
        }
    }

    @Test func theSignInButtonsShareOneHeight() {
        let faceID = Button {
        } label: {
            SignInButtonLabel(title: Text("Sign in with Face ID"), height: 50) { Image(systemName: "faceid") }
        }
        let google = Button {
        } label: {
            SignInButtonLabel(title: Text("Continue with Google"), height: 50) { Image(systemName: "g.circle") }
        }
        #expect(abs(size(faceID.buttonStyle(.dsSignInPrimary)).height - 50) < 1)
        #expect(abs(size(google.buttonStyle(.dsOutline(fullWidth: true))).height - 50) < 1)
    }

    @Test func theSignInButtonTitlesAreSentenceCase() {
        #expect(BiometryKind.faceID.signInTitle == "Sign in with Face ID")
    }

    @Test func theSignInTitleMatchesSignInWithApple() {
        // Apple's button title is 43% of its height: 21.5 pt at 50.
        #expect(DS.Typography.signInButton(height: 50) == .system(size: 22, weight: .medium))
        #expect(SignInButtonLabel<EmptyView>.iconSize(50) == 20)
    }
}
