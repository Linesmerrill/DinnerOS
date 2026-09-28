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

    /// The summary for a run, or for a household that has linked but never run one. `now` is
    /// what "next batch in about a minute" is measured from.
    ///
    /// The copy rules: say what is happening and what to do, in the fewest words; one idea per
    /// sentence; no dash asides; no reassurance padding.
    static func summary(
        for job: MealKitImportJob?, service: MealKitService, now: Date = .now
    ) -> Summary {
        guard let job else {
            return Summary(
                title: String(localized: "No imports yet"),
                detail: String(localized: "Import your \(service.displayName) recipes any time."),
                symbol: "tray", needsAttention: false)
        }
        switch job.state {
        case .queued:
            // The moment between queueing and the server picking it up: seconds, normally.
            return Summary(
                title: String(localized: "Starting your import"),
                detail: job.recipesFound > 0
                    ? String(localized: "\(job.recipesFound) recipes to import.") + " " + nextStep(job, now: now)
                    : String(localized: "Getting your \(service.displayName) recipes."),
                symbol: "clock", needsAttention: false)
        case .running:
            return Summary(
                title: String(localized: "Importing your recipes"),
                detail: progressDetail(job, service: service),
                symbol: "arrow.trianglehead.2.clockwise", needsAttention: false)
        case .waiting:
            return Summary(
                title: String(localized: "Importing your recipes"),
                detail: importedCount(job) + " " + nextStep(job, now: now),
                symbol: "pause.circle", needsAttention: false)
        case .finished:
            return Summary(
                title: String(localized: "Import finished"), detail: finishedDetail(job, service: service),
                symbol: job.failures.isEmpty ? "checkmark.circle" : "exclamationmark.triangle",
                needsAttention: false)
        case .failed:
            return Summary(
                title: String(localized: "Import stopped"),
                detail: job.lastError?.message
                    ?? String(localized: "Something went wrong. Your recipes weren't changed."),
                symbol: "exclamationmark.triangle", needsAttention: true)
        case .canceled:
            return Summary(
                title: String(localized: "Import cancelled"),
                detail: String(localized: "Stopped before it finished."),
                symbol: "xmark.circle", needsAttention: false)
        }
    }

    /// "12 of 740 recipes imported. You can close the app." while a batch works, and "Getting
    /// your HelloFresh recipes." before there is a total to count against.
    ///
    /// The second sentence is not padding: the import runs on the server, and a member watching
    /// 12 of 740 should know closing the app costs them nothing.
    static func progressDetail(_ job: MealKitImportJob, service: MealKitService) -> String {
        guard job.recipesFound > 0 else {
            return String(localized: "Getting your \(service.displayName) recipes.")
        }
        return importedCount(job) + " " + String(localized: "You can close the app.")
    }

    /// "12 of 740 recipes imported."
    static func importedCount(_ job: MealKitImportJob) -> String {
        String(localized: "\(job.recipesDone) of \(job.recipesFound) recipes imported.")
    }

    /// What happens next for a queued run, as one sentence: "Starting now.", "Next batch in about
    /// a minute.", "Trying again in about 4 minutes."
    ///
    /// It is measured from the server's `nextRunAt`. A time already passed reads as "now": the
    /// server picks a due run up within seconds of being asked about it, so saying anything
    /// gloomier would be the dishonest direction.
    static func nextStep(_ job: MealKitImportJob, now: Date = .now) -> String {
        let retrying = job.lastError != nil
        let firstBatch = job.state == .queued
        let wait = job.nextRunAt.map { $0.timeIntervalSince(now) } ?? 0
        guard wait > 5 else {
            if firstBatch { return String(localized: "Starting now.") }
            return retrying ? String(localized: "Trying again now.") : String(localized: "Next batch starting now.")
        }
        let when = approximately(wait)
        if retrying { return String(localized: "Trying again \(when).") }
        if firstBatch { return String(localized: "Starting \(when).") }
        return String(localized: "Next batch \(when).")
    }

    /// "in about a minute", "in about 4 minutes", "in about 2 hours".
    static func approximately(_ seconds: TimeInterval) -> String {
        if seconds < 90 { return String(localized: "in about a minute") }
        let minutes = Int((seconds / 60).rounded())
        if minutes < 60 { return String(localized: "in about \(minutes) minutes") }
        let hours = max(1, Int((seconds / 3600).rounded()))
        return hours == 1 ? String(localized: "in about an hour") : String(localized: "in about \(hours) hours")
    }

    /// What to say about order history that has not been read yet, or nothing when there is
    /// none left.
    ///
    /// It says what to do, because only the member can do it: the rest of the history needs
    /// another sign-in in their own browser session. Nothing on the server fetches it for them,
    /// so nothing here says it will.
    static func moreHistoryNote(
        for job: MealKitImportJob?, history: MealKitImportHistory?, service: MealKitService
    ) -> String? {
        let more = history?.moreToFetch == true || job?.moreHistoryToFetch == true
        guard more else { return nil }
        let reached = history?.earliestWeek ?? job?.harvest?.earliestWeek ?? ""
        if let since = monthAndYear(of: reached) {
            return String(
                localized: "Got your \(service.displayName) orders back to \(since). Tap Import Again for older ones.")
        }
        return String(localized: "You have older \(service.displayName) orders. Tap Import Again to get them.")
    }

    /// "March 2024" for an ISO week such as `2024-W12`, or `nil` when it is not one.
    static func monthAndYear(of week: String) -> String? {
        guard let monday = MealKitHarvestPlan.mondayOf(week) else { return nil }
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .iso8601)
        formatter.timeZone = TimeZone(identifier: "UTC") ?? .gmt
        formatter.setLocalizedDateFormatFromTemplate("MMMMy")
        return formatter.string(from: monday)
    }

    /// One line per kind of outcome, straight from the job's counts: what is new, what only
    /// gained order history, what was already current, and what failed.
    ///
    /// "Added" is never used for an updated recipe. A re-import of a long history updates
    /// hundreds of recipes and creates a handful, and counting both as "added" told a member
    /// 676 recipes were new when 5 were.
    static func finishedDetail(_ job: MealKitImportJob, service: MealKitService) -> String {
        var lines: [String] = []
        if job.imported > 0 {
            lines.append(
                job.imported == 1
                    ? String(localized: "1 new recipe.")
                    : String(localized: "\(job.imported) new recipes."))
        }
        if job.updated > 0 {
            lines.append(String(localized: "\(job.updated) updated with your order history."))
        }
        if job.unchanged > 0 {
            lines.append(String(localized: "\(job.unchanged) already up to date."))
        }
        if !job.failures.isEmpty {
            lines.append(String(localized: "\(job.failures.count) couldn't be imported."))
        }
        if lines.isEmpty {
            return String(localized: "Your \(service.displayName) recipes were already up to date.")
        }
        return lines.joined(separator: "\n")
    }

    /// What the sign-in actually does, shown under the button that starts it: two short lines,
    /// one about the password and one about what is kept. It is the only place a screen says it,
    /// so no footer repeats it.
    static func credentialExplanation(for service: MealKitService) -> String {
        String(localized: "You sign in on \(service.displayName)'s own page. We never see your password.")
            + "\n" + String(localized: "Nothing about your \(service.displayName) account is saved.")
    }
}
