import Foundation
import Synchronization

/// Runs work that may never finish against a time limit. WebKit's cookie store can leave a read
/// unanswered, and the sign-in sheet must never wait on one forever.
nonisolated enum MealKitDeadline {
    /// The work's result, or `nil` when it hasn't answered within `limit`. Work that never
    /// answers is left behind rather than awaited: it can't be cancelled.
    static func value<T: Sendable>(
        within limit: Duration, _ work: @escaping @MainActor @Sendable () async -> T
    ) async -> T? {
        await withCheckedContinuation { (continuation: CheckedContinuation<T?, Never>) in
            let once = ResumeOnce(continuation)
            Task { @MainActor in once.resume(await work()) }
            // Off the main actor, so a busy main thread can't hold up the limit.
            Task.detached {
                try? await Task.sleep(for: limit)
                once.resume(nil)
            }
        }
    }

    /// Resumes a continuation with the first answer and ignores the rest.
    private nonisolated final class ResumeOnce<T: Sendable>: Sendable {
        private let continuation: Mutex<CheckedContinuation<T?, Never>?>

        init(_ continuation: CheckedContinuation<T?, Never>) {
            self.continuation = Mutex(continuation)
        }

        func resume(_ value: T?) {
            let waiting = continuation.withLock { stored in
                defer { stored = nil }
                return stored
            }
            waiting?.resume(returning: value)
        }
    }
}
