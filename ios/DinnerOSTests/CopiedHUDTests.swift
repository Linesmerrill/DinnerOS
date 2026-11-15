import Testing
import UIKit

@testable import DinnerOS

/// Every copy goes through `Clipboard.copy`, which puts the text on the clipboard and shows the
/// Copied HUD.
@MainActor
struct CopiedHUDTests {
    @Test func copyingWritesTheTextAndShowsTheHUD() {
        let before = CopiedHUD.shared.copies
        Clipboard.copy("6 oz Carrots\n1 Lime")
        #expect(UIPasteboard.general.string == "6 oz Carrots\n1 Lime")
        #expect(CopiedHUD.shared.copies == before + 1)
        Clipboard.copy("ABC123", expiresAfter: 600)
        #expect(UIPasteboard.general.string == "ABC123")
        #expect(CopiedHUD.shared.copies == before + 2)
    }
}
