import Foundation
import Observation
import StoreKit

/// Where this copy of the app came from: the App Store, TestFlight, or Xcode. Feedback & Bugs
/// points testers at TestFlight, so it shows only in development builds and to beta households.
enum BuildChannel: Equatable, Sendable {
    case appStore
    case testFlight
    case development

    /// Beta feedback belongs to TestFlight; App Store builds don't mention it. App Review runs
    /// the same binary in the sandbox, so StoreKit says `.testFlight` there too: a sandbox
    /// build shows the TestFlight steps only to a household that tested the beta (its Plus
    /// comp says so), which a reviewer's new account never is.
    func showsBetaFeedback(testedTheBeta: Bool) -> Bool {
        switch self {
        case .development: true
        case .testFlight: testedTheBeta
        case .appStore: false
        }
    }

    /// Whether a comp's Plus line may thank the household for testing: in development builds,
    /// and in TestFlight for a household with the beta comp. Never in an App Store build, and
    /// never while the channel is unknown (`nil`), which counts as the App Store (build 121).
    static func showsTesterThanks(_ channel: BuildChannel?, comp: PlusComp?) -> Bool {
        guard let channel, let comp else { return false }
        return channel.showsBetaFeedback(testedTheBeta: comp.isBeta)
    }

    /// The channel from what StoreKit says, falling back to the receipt's file name when
    /// StoreKit can't answer (offline on first launch). Unknown counts as the App Store, so a
    /// reviewer never sees beta wording.
    static func from(environment: AppStore.Environment?, receiptName: String?, debug: Bool) -> BuildChannel {
        if debug { return .development }
        switch environment {
        case .xcode?: return .development
        case .sandbox?: return .testFlight
        case .production?: return .appStore
        default: break
        }
        return receiptName == "sandboxReceipt" ? .testFlight : .appStore
    }

    /// Asks StoreKit where this build came from.
    static func lookUp() async -> BuildChannel {
        #if DEBUG
            let debug = true
        #else
            let debug = false
        #endif
        var environment: AppStore.Environment?
        if !debug, let result = try? await AppTransaction.shared {
            switch result {
            case .verified(let transaction), .unverified(let transaction, _):
                environment = transaction.environment
            }
        }
        return from(
            environment: environment, receiptName: Bundle.main.appStoreReceiptURL?.lastPathComponent, debug: debug)
    }
}

/// The build's channel, looked up once at launch. Nil until then, which hides beta rows.
@MainActor @Observable
final class BuildChannelStore {
    static let shared = BuildChannelStore()

    private(set) var channel: BuildChannel?

    func resolve() async {
        guard channel == nil else { return }
        #if DEBUG
            // Screenshot QA (build 121) shows channel-dependent screens as the App Store build
            // has them: QA_BUILD_CHANNEL=appStore. Debug builds only.
            if let forced = Self.forced(ProcessInfo.processInfo.environment["QA_BUILD_CHANNEL"]) {
                channel = forced
                return
            }
        #endif
        channel = await BuildChannel.lookUp()
    }

    /// A channel named by QA (Debug builds only), or nil.
    nonisolated static func forced(_ name: String?) -> BuildChannel? {
        switch name {
        case "appStore": .appStore
        case "testFlight": .testFlight
        case "development": .development
        default: nil
        }
    }
}
