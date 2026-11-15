import Foundation
import Testing

@testable import DinnerOS

/// The Walmart chooser never sits on a blank page: it says it's loading, or that it didn't load.
@MainActor
struct WalmartPageStateTests {
    @Test func loadingUntilThePagePaints() {
        let page = WalmartBrowserPage()
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        #expect(page.state == .loading)
        page.committed()
        #expect(page.state == .shown)
    }

    @Test func aFailedLoadSaysSoAndTryAgainStartsOver() {
        let page = WalmartBrowserPage()
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        page.failed(URLError(.notConnectedToInternet))
        #expect(page.state == .failed)
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        #expect(page.state == .loading)
    }

    @Test func aReplacedLoadIsntAFailure() {
        let page = WalmartBrowserPage()
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        page.failed(URLError(.cancelled))
        #expect(page.state == .loading)
    }

    @Test func aLaterHiccupDoesntHideAPageAlreadyShown() {
        let page = WalmartBrowserPage()
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        page.committed()
        page.failed(URLError(.timedOut))
        #expect(page.state == .shown)
    }
}

@MainActor
struct WalmartPageCrashTests {
    @Test func aCrashedPageSaysSoEvenAfterItShowed() {
        let page = WalmartBrowserPage()
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        page.committed()
        page.crashed()
        #expect(page.state == .failed)
        page.load(URL(string: "https://www.walmart.com/search?q=lime")!)
        #expect(page.state == .loading)
    }
}
