import Foundation

/// "Step 2 done": a free month checklist step this device just saw finish (decision 729).
nonisolated struct ReferralStepMoment: Hashable, Sendable, Identifiable {
    let stepID: String
    /// Where the step sits in the checklist, counting from 1.
    let number: Int
    /// How many steps are done now, and how many there are.
    let done: Int
    let total: Int
    /// What's left, in the server's words: "Plan a dinner to unlock your free month".
    let message: String

    var id: String { stepID }
    var title: String { String(localized: "Step \(number) done") }
    var countText: String { String(localized: "\(done) of \(total) done") }
    /// What VoiceOver says when the card shows.
    var announcement: String { "\(title). \(countText). \(message)" }
}

/// Which checklist step just finished, so each one gets its moment once (decision 729).
nonisolated enum ReferralMoments {
    /// The step to celebrate, and the steps to remember as seen. `seen` is nil the first time
    /// this device sees the checklist: whatever is already done then is remembered without a
    /// card, so a second phone or an app update brings no surprise. When several steps finish
    /// between checks, the last of them in checklist order gets the card.
    static func stepJustDone(_ progress: ReferralProgress, seen: Set<String>?) -> (
        moment: ReferralStepMoment?, seen: Set<String>
    ) {
        let doneSteps = progress.steps.filter(\.done)
        let remembered = (seen ?? []).union(doneSteps.map(\.id))
        guard let seen, progress.isPending,
            let index = progress.steps.lastIndex(where: { $0.done && !seen.contains($0.id) })
        else { return (nil, remembered) }
        let moment = ReferralStepMoment(
            stepID: progress.steps[index].id, number: index + 1, done: doneSteps.count,
            total: progress.steps.count, message: progress.message)
        return (moment, remembered)
    }

    /// How long a step's card stays before it fades: long enough to read, short enough to
    /// stay out of the way.
    static let stepSeconds: Double = 3
    /// The free month card is the bigger moment, so it stays longer.
    static let unlockedSeconds: Double = 8

    /// `base`, doubled while VoiceOver reads it out. A debug build can set it for UI tests,
    /// which can look a few seconds late.
    static func seconds(_ base: Double, voiceOver: Bool) -> Double {
        #if DEBUG
            if let seconds = ProcessInfo.processInfo.environment["QA_MOMENT_SECONDS"].flatMap(Double.init) {
                return seconds
            }
        #endif
        return voiceOver ? base * 2 : base
    }
}
