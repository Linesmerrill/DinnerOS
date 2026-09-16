import CoreGraphics
import Foundation
import ImageIO
import Synchronization
import Testing
import UIKit
import UniformTypeIdentifiers

@testable import DinnerOS

/// A synthetic PNG. No fixture files and no network: the bytes are drawn here.
///
/// `nonisolated` because the fake loader's handlers are `@Sendable` and run off the main actor.
private nonisolated func pngData(width: Int, height: Int) -> Data {
    guard
        let context = CGContext(
            data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
    else { return Data() }
    context.setFillColor(red: 1, green: 0.5, blue: 0, alpha: 1)
    context.fill(CGRect(x: 0, y: 0, width: width, height: height))
    guard
        let image = context.makeImage(),
        let buffer = CFDataCreateMutable(nil, 0),
        let destination = CGImageDestinationCreateWithData(buffer, UTType.png.identifier as CFString, 1, nil)
    else { return Data() }
    CGImageDestinationAddImage(destination, image, nil)
    guard CGImageDestinationFinalize(destination) else { return Data() }
    return buffer as Data
}

/// An `ImageDataLoading` that answers from memory and counts what it was asked for. The
/// handler receives the attempt number, so a test can fail the first try and succeed the next.
private final class FakeImageData: ImageDataLoading {
    private struct State {
        var calls = 0
        var cancellations = 0
    }

    private let state = Mutex(State())
    private let respond: @Sendable (URL, Int) async throws -> Data

    init(respond: @escaping @Sendable (URL, Int) async throws -> Data) {
        self.respond = respond
    }

    var calls: Int { state.withLock { $0.calls } }
    var cancellations: Int { state.withLock { $0.cancellations } }

    func data(for url: URL) async throws -> Data {
        let attempt = state.withLock { current -> Int in
            current.calls += 1
            return current.calls
        }
        do {
            return try await respond(url, attempt)
        } catch is CancellationError {
            state.withLock { $0.cancellations += 1 }
            throw CancellationError()
        }
    }
}

private func testURL(_ string: String = "https://img.example.test/f_auto,q_auto,w_1200/a.jpg") throws -> URL {
    try #require(URL(string: string))
}

private func makeLoader(_ data: FakeImageData) -> ImageLoader {
    ImageLoader(data: data, cache: ImageMemoryCache())
}

struct ImageLoaderCacheTests {
    @Test func cachesOnePhotoPerURLAndSize() async throws {
        let data = FakeImageData { _, _ in pngData(width: 600, height: 400) }
        let loader = makeLoader(data)
        let url = try testURL()
        let small = ImageKey(url: url, pixelSize: 320)
        let large = ImageKey(url: url, pixelSize: 800)

        #expect(await loader.image(for: small) != nil)
        #expect(await loader.cachedImage(for: small) != nil)
        // The same URL at another size is a different entry, not a hit.
        #expect(await loader.cachedImage(for: large) == nil)
        #expect(await loader.image(for: large) != nil)
        #expect(data.calls == 2)

        // A size that's already decoded does no work at all.
        #expect(await loader.image(for: small) != nil)
        #expect(data.calls == 2)
        let stats = await loader.statistics()
        #expect(stats.hits == 1)
        #expect(stats.misses == 2)
        #expect(stats.hitRate == 1.0 / 3.0)
    }

    @Test func separatesPhotosByURL() async throws {
        let data = FakeImageData { _, _ in pngData(width: 400, height: 300) }
        let loader = makeLoader(data)
        let first = ImageKey(url: try testURL(), pixelSize: 320)
        let second = ImageKey(url: try testURL("https://img.example.test/w_1200/b.jpg"), pixelSize: 320)

        #expect(await loader.image(for: first) != nil)
        #expect(await loader.cachedImage(for: second) == nil)
        #expect(data.calls == 1)
    }

    @Test func decodesToTheKeySize() async throws {
        let data = FakeImageData { _, _ in pngData(width: 1_200, height: 900) }
        let loader = makeLoader(data)
        let image = try #require(await loader.image(for: ImageKey(url: try testURL(), pixelSize: 160)))
        // Downsampled, not the 1200-pixel original.
        #expect(max(image.size.width, image.size.height) <= 160)
    }

    /// `NSCache`'s cost limit is advisory — it decides when to evict — so the deterministic
    /// half is tested here: the cost that's reported to it, and the memory-warning path that
    /// empties the cache outright.
    @Test func emptiesOnAMemoryWarning() throws {
        // Its own notification centre. The suite runs tests in parallel, and a memory warning
        // posted to the default one would empty the cache of every test running alongside this.
        let center = NotificationCenter()
        let cache = ImageMemoryCache(notificationCenter: center)
        let key = ImageKey(url: try testURL(), pixelSize: 320)
        let image = try #require(ImageDownsampler.image(from: pngData(width: 600, height: 400), maxPixelSize: 320))
        cache.store(image, for: key)
        #expect(cache.image(for: key) != nil)
        #expect(ImageDownsampler.cost(of: image) > 0)

        center.post(name: UIApplication.didReceiveMemoryWarningNotification, object: nil)
        #expect(cache.image(for: key) == nil)
    }

    @Test func reportsASmallerCostForASmallerDecode() throws {
        let bytes = pngData(width: 1_200, height: 900)
        let large = try #require(ImageDownsampler.image(from: bytes, maxPixelSize: 1_200))
        let small = try #require(ImageDownsampler.image(from: bytes, maxPixelSize: 160))
        #expect(ImageDownsampler.cost(of: small) < ImageDownsampler.cost(of: large))
    }
}

struct ImageLoaderCoalescingTests {
    @Test func oneFetchServesEveryCardAskingForThePhoto() async throws {
        let data = FakeImageData { _, _ in
            try await Task.sleep(for: .milliseconds(150))
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        let key = ImageKey(url: try testURL(), pixelSize: 320)

        await withTaskGroup(of: Bool.self) { group in
            for _ in 0..<5 {
                group.addTask { await loader.image(for: key) != nil }
            }
            for await loaded in group {
                #expect(loaded)
            }
        }

        #expect(data.calls == 1)
        #expect(await loader.statistics().coalesced == 4)
    }

    @Test func aPrefetchedPhotoIsNotFetchedTwice() async throws {
        let data = FakeImageData { _, _ in
            try await Task.sleep(for: .milliseconds(100))
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        let key = ImageKey(url: try testURL(), pixelSize: 320)

        await loader.prefetch([key])
        #expect(await loader.image(for: key) != nil)
        #expect(data.calls == 1)
    }
}

struct ImageLoaderCancellationTests {
    @Test func scrollingPastTheLastWaiterCancelsTheFetch() async throws {
        let data = FakeImageData { _, _ in
            try await Task.sleep(for: .seconds(5))
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        let key = ImageKey(url: try testURL(), pixelSize: 320)

        let request = Task { await loader.image(for: key) }
        try await Task.sleep(for: .milliseconds(100))
        request.cancel()
        #expect(await request.value == nil)

        try await Task.sleep(for: .milliseconds(200))
        #expect(data.cancellations == 1)
        // A cancelled fetch isn't a failure, and leaves nothing cached.
        #expect(await loader.cachedImage(for: key) == nil)
        #expect(await loader.statistics().failures == 0)
    }

    @Test func aFetchSurvivesOneCardLeavingWhileAnotherWaits() async throws {
        let data = FakeImageData { _, _ in
            try await Task.sleep(for: .milliseconds(400))
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        let key = ImageKey(url: try testURL(), pixelSize: 320)

        let leaving = Task { await loader.image(for: key) }
        let staying = Task { await loader.image(for: key) }
        try await Task.sleep(for: .milliseconds(100))
        leaving.cancel()

        // The card still on screen gets its photo; the fetch was not cancelled under it.
        #expect(await staying.value != nil)
        #expect(data.cancellations == 0)
    }

    @Test func droppingAPrefetchCancelsIt() async throws {
        let data = FakeImageData { _, _ in
            try await Task.sleep(for: .seconds(5))
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        let key = ImageKey(url: try testURL(), pixelSize: 320)

        await loader.prefetch([key])
        try await Task.sleep(for: .milliseconds(100))
        await loader.cancelPrefetch([key])
        try await Task.sleep(for: .milliseconds(200))
        #expect(data.cancellations == 1)
    }
}

struct ImageLoaderFailureTests {
    @Test func retriesOnceOnATransientFailure() async throws {
        let data = FakeImageData { _, attempt in
            if attempt == 1 { throw URLError(.timedOut) }
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        #expect(await loader.image(for: ImageKey(url: try testURL(), pixelSize: 320)) != nil)
        #expect(data.calls == 2)
    }

    @Test func givesUpAfterTheRetry() async throws {
        let data = FakeImageData { _, _ in throw URLError(.networkConnectionLost) }
        let loader = makeLoader(data)
        #expect(await loader.image(for: ImageKey(url: try testURL(), pixelSize: 320)) == nil)
        #expect(data.calls == 2)
        #expect(await loader.statistics().failures == 1)
    }

    @Test func neverRetriesAPhotoThatIsGone() async throws {
        let data = FakeImageData { _, _ in throw ImageLoadError.server(status: 404) }
        let loader = makeLoader(data)
        #expect(await loader.image(for: ImageKey(url: try testURL(), pixelSize: 320)) == nil)
        #expect(data.calls == 1)
    }

    @Test func retriesAServerThatIsBrieflyUnhappy() async throws {
        let data = FakeImageData { _, attempt in
            if attempt == 1 { throw ImageLoadError.server(status: 503) }
            return pngData(width: 400, height: 300)
        }
        let loader = makeLoader(data)
        #expect(await loader.image(for: ImageKey(url: try testURL(), pixelSize: 320)) != nil)
        #expect(data.calls == 2)
    }

    @Test func bytesThatArentAnImageFail() async throws {
        let data = FakeImageData { _, _ in Data("not an image".utf8) }
        let loader = makeLoader(data)
        #expect(await loader.image(for: ImageKey(url: try testURL(), pixelSize: 320)) == nil)
        // Malformed bytes are not a transient failure, so there's no second try.
        #expect(data.calls == 1)
    }
}

struct ImageKeyTests {
    @Test func sizesTheURLAndTheDecodeToTheSameBucket() throws {
        let key = try #require(ImageKey(url: try testURL(), pointWidth: 220, scale: 3))
        // 220 points at @3x is 660 pixels, which rounds up to the 800 bucket.
        #expect(key.pixelSize == 800)
        #expect(key.url.absoluteString.contains("w_800"))
    }

    @Test func honorsTheDisplayScale() throws {
        let atTwo = try #require(ImageKey(url: try testURL(), pointWidth: 160, scale: 2))
        let atThree = try #require(ImageKey(url: try testURL(), pointWidth: 160, scale: 3))
        #expect(atTwo.pixelSize == 320)
        #expect(atThree.pixelSize == 480)
        #expect(atTwo != atThree)
    }

    @Test func fallsBackWhenTheWidthIsntKnownYet() throws {
        let key = try #require(ImageKey(url: try testURL(), pointWidth: 0, scale: 3))
        #expect(key.pixelSize == ImageKey.fallbackBucket)
        // Never the 1200-pixel original just because the layout hasn't measured yet.
        #expect(key.url.absoluteString.contains("w_640"))
    }

    @Test func aRecipeWithNoPhotoHasNoKey() {
        #expect(ImageKey(url: nil, pointWidth: 320, scale: 3) == nil)
    }

    @Test func clampsADegenerateSize() throws {
        #expect(ImageKey(url: try testURL(), pixelSize: 0).pixelSize == 1)
    }
}

struct ImagePresentationTests {
    @Test func drawsThePhotoOnceItIsLoaded() {
        #expect(
            ImagePresentation.state(hasImage: true, hasKey: true, didFail: false, delayElapsed: true) == .image)
    }

    @Test func showsNothingForTheFirstInstantOfALoad() {
        // A cached photo arrives on the first frame, so a placeholder here would only flash.
        #expect(
            ImagePresentation.state(hasImage: false, hasKey: true, didFail: false, delayElapsed: false) == .empty)
    }

    @Test func showsTheShimmerOnceTheLoadIsSlow() {
        #expect(
            ImagePresentation.state(hasImage: false, hasKey: true, didFail: false, delayElapsed: true)
                == .placeholder)
    }

    @Test func showsTheGlyphImmediatelyWithoutAPhoto() {
        for elapsed in [false, true] {
            #expect(
                ImagePresentation.state(hasImage: false, hasKey: false, didFail: false, delayElapsed: elapsed)
                    == .fallback)
        }
    }

    @Test func settlesOnTheGlyphAfterAFailure() {
        #expect(
            ImagePresentation.state(hasImage: false, hasKey: true, didFail: true, delayElapsed: true) == .fallback)
    }

    @Test func waitsAShortTimeBeforeAdmittingToALoad() {
        #expect(ImagePresentation.placeholderDelay == .milliseconds(100))
    }
}

struct ImagePrefetchWindowTests {
    private func urls(_ count: Int) -> [URL?] {
        (0..<count).map { URL(string: "https://img.example.test/w_1200/\($0).jpg") }
    }

    @Test func warmsTheNextFewPhotos() {
        let keys = ImagePrefetchWindow.keys(after: 0, in: urls(20), pointWidth: 220, scale: 3)
        #expect(keys.count == ImagePrefetchWindow.count)
        // Starts at the next card, not the one that just appeared.
        #expect(keys.first?.url.absoluteString.contains("/1.jpg") == true)
        #expect(keys.last?.url.absoluteString.contains("/5.jpg") == true)
    }

    @Test func stopsAtTheEndOfTheList() {
        #expect(ImagePrefetchWindow.keys(after: 8, in: urls(10), pointWidth: 220, scale: 3).count == 1)
        #expect(ImagePrefetchWindow.keys(after: 9, in: urls(10), pointWidth: 220, scale: 3).isEmpty)
        #expect(ImagePrefetchWindow.keys(after: 0, in: [], pointWidth: 220, scale: 3).isEmpty)
        #expect(ImagePrefetchWindow.keys(after: -1, in: urls(10), pointWidth: 220, scale: 3).isEmpty)
    }

    @Test func skipsRecipesWithoutPhotos() {
        var list = urls(10)
        list[1] = nil
        list[2] = nil
        let keys = ImagePrefetchWindow.keys(after: 0, in: list, pointWidth: 220, scale: 3)
        #expect(keys.count == 3)
    }

    @Test func warmsTheSizeTheCardWillAskFor() {
        let keys = ImagePrefetchWindow.keys(after: 0, in: urls(10), pointWidth: 220, scale: 3)
        let card = ImageKey(url: urls(10)[1], pointWidth: 220, scale: 3)
        #expect(keys.first == card)
    }
}
