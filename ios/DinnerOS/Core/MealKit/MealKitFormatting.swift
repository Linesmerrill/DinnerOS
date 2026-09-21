import Foundation

/// What the import screens say for a given run. Pure so the wording is tested rather than
/// checked by eye on a simulator.
nonisolated enum MealKitFormatting {
    /// A one-line headline and a sentence under it.
    struct Summary: Equatable, Sendable {
        let title: String
        let detail: String
        /// The SF Symbol for the row.
        let symbol: String
        /// True when the member has to do something: sign in again, or retry.
        let needsAttention: Bool
    }

    /// The summary for a run, or for a household that has linked but never run one.
    static func summary(for job: MealKitImportJob?, service: MealKitService) -> Summary {
        guard let job else {
            return Summary(
                title: String(localized: "No imports yet"),
                detail: String(localized: "Import your \(service.displayName) recipes whenever you like."),
                symbol: "tray", needsAttention: false)
        }
        switch job.state {
        case .queued:
            return Summary(
                title: String(localized: "Import queued"),
                detail: String(
                    localized:
                        "We'll fetch your \(service.displayName) recipes in the background. You can close the app."),
                symbol: "clock", needsAttention: false)
        case .running:
            return Summary(
                title: String(localized: "Importing your recipes"),
                detail: progressDetail(job, service: service),
                symbol: "arrow.trianglehead.2.clockwise", needsAttention: false)
        case .finished:
            return Summary(
                title: finishedTitle(job), detail: finishedDetail(job, service: service),
                symbol: job.failures.isEmpty ? "checkmark.circle" : "exclamationmark.triangle",
                needsAttention: false)
        case .failed:
            return Summary(
                title: String(localized: "Import stopped"),
                detail: job.lastError?.message
                    ?? String(localized: "Something went wrong and we stopped. Your recipes weren't changed."),
                symbol: "exclamationmark.triangle", needsAttention: true)
        case .canceled:
            return Summary(
                title: String(localized: "Import cancelled"),
                detail: String(localized: "This import was stopped before it finished."),
                symbol: "xmark.circle", needsAttention: false)
        }
    }

    /// "12 of 48 recipes" once the order history has been read, and an honest
    /// "Reading your order history" before it — no invented total, no fake bar.
    static func progressDetail(_ job: MealKitImportJob, service: MealKitService) -> String {
        guard job.recipesFound > 0 else {
            return String(localized: "Getting your \(service.displayName) recipes ready…")
        }
        return String(localized: "\(job.recipesDone) of \(job.recipesFound) recipes")
    }

    private static func finishedTitle(_ job: MealKitImportJob) -> String {
        job.recipesAdded == 1
            ? String(localized: "1 recipe added")
            : String(localized: "\(job.recipesAdded) recipes added")
    }

    private static func finishedDetail(_ job: MealKitImportJob, service: MealKitService) -> String {
        var parts: [String] = []
        if job.unchanged > 0 {
            parts.append(
                job.unchanged == 1
                    ? String(localized: "1 was already in your library")
                    : String(localized: "\(job.unchanged) were already in your library"))
        }
        if !job.failures.isEmpty {
            parts.append(
                job.failures.count == 1
                    ? String(localized: "1 couldn't be imported")
                    : String(localized: "\(job.failures.count) couldn't be imported"))
        }
        if parts.isEmpty {
            return String(localized: "Everything you ordered from \(service.displayName) is in your library.")
        }
        return parts.joined(separator: ", ") + "."
    }

    /// What the sign-in actually does, shown under the button that starts it. A sign-in to
    /// someone else's account from inside another app deserves saying plainly who sees what.
    static func credentialExplanation(for service: MealKitService) -> String {
        String(
            localized: """
                You sign in on \(service.displayName)'s own page, so your password never reaches \
                DinnerOS. We read your past orders there and keep only the recipes — nothing about \
                your account is saved.
                """)
    }
}
