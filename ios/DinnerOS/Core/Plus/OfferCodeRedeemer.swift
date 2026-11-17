import StoreKit
import UIKit

/// What Apple's redeem sheet did, as far as StoreKit says.
nonisolated enum OfferCodeSheetResult: Hashable, Sendable {
    /// StoreKit handed back a transaction: the code went through (iOS 27 and later).
    case redeemed
    /// The sheet closed. Older systems don't say whether a code went in.
    case closed
}

/// Puts a free month's code where the person can use it and opens Apple's redeem sheet
/// (decision 755). Behind a protocol so tests can stand in for StoreKit. Nothing here logs
/// the code.
@MainActor
protocol OfferCodeRedeeming: AnyObject {
    /// Puts `code` on the clipboard for a few minutes, on this device only.
    func copy(_ code: String)
    /// Shows Apple's redeem sheet and returns once it closes. Throws when it can't show.
    func presentRedeemSheet() async throws -> OfferCodeSheetResult
    /// Opens Apple's redeem page, with the code filled in. Returns whether it opened.
    func open(_ url: URL) async -> Bool
}

/// Why the redeem sheet couldn't show.
nonisolated enum OfferCodeRedeemError: Error, Equatable {
    case noWindow
}

/// The real one: the clipboard, StoreKit's redeem sheet, and the App Store's redeem page.
@MainActor
final class StoreKitOfferCodeRedeemer: OfferCodeRedeeming {
    /// How long the code stays on the clipboard.
    static let clipboardSeconds: TimeInterval = 10 * 60

    func copy(_ code: String) {
        Clipboard.copy(PlusText.copyableCode(code), expiresAfter: Self.clipboardSeconds)
    }

    func presentRedeemSheet() async throws -> OfferCodeSheetResult {
        guard let scene = Self.activeScene() else { throw OfferCodeRedeemError.noWindow }
        // The scene form builds with every Xcode we use; CI's SDK doesn't have the iOS 27
        // view-controller form yet. It doesn't say whether a code was redeemed, so the caller
        // refreshes Plus after it closes either way.
        try await AppStore.presentOfferCodeRedeemSheet(in: scene)
        return .closed
    }

    func open(_ url: URL) async -> Bool {
        await UIApplication.shared.open(url)
    }

    /// The scene in front, where the sheet shows.
    static func activeScene() -> UIWindowScene? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
    }

    /// The app's own front screen: the normal window's topmost presented controller, not the
    /// windows the moments and the Copied HUD float in.
    static func topViewController(in scene: UIWindowScene) -> UIViewController? {
        let window =
            scene.windows.first { $0.isKeyWindow && $0.windowLevel == .normal }
            ?? scene.windows.first { $0.windowLevel == .normal }
        var top = window?.rootViewController
        while let presented = top?.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        return top
    }
}
