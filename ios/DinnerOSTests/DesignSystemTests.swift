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

    @Test func theSignInTitleMatchesSignInWithApple() {
        // Apple's button title is 43% of its height: 21.5 pt at 50.
        #expect(DS.Typography.signInButton(height: 50) == .system(size: 22, weight: .medium))
        #expect(SignInButtonLabel<EmptyView>.iconSize(50) == 20)
    }
}
