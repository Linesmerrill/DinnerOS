import SwiftUI

/// How testers send feedback, report a bug, or share a crash. DinnerOS is in TestFlight, which
/// already collects all three with screenshots and device details, so this explains that
/// rather than building a second channel.
struct FeedbackView: View {
    var body: some View {
        List {
            Section {
                Text("Something confusing, broken, or missing? We want to hear it. Every note is read.")
                    .fixedSize(horizontal: false, vertical: true)
            }
            Section {
                step(1, "Take a screenshot of the screen you mean.")
                step(2, "Tap the screenshot in the corner, then tap Done.")
                step(3, "Choose Share Beta Feedback, add a note, and send.")
            } header: {
                Text("Send Feedback or Report a Bug")
            } footer: {
                Text("Your screenshot, note, and device details go straight to us.")
            }
            Section {
                if let testFlight = Self.testFlightURL {
                    Link(destination: testFlight) {
                        DSIconRow(systemImage: "airplane") { Text("Open TestFlight") }
                    }
                }
                Text("In TestFlight, open DinnerOS and tap Send Beta Feedback.")
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
                    .fixedSize(horizontal: false, vertical: true)
            } header: {
                Text("No Screenshot Needed")
            }
            Section {
                Text(
                    "If the app closes suddenly, TestFlight asks to send a crash report next time you open it. Tap Share and add what you were doing."
                )
                .fixedSize(horizontal: false, vertical: true)
            } header: {
                Text("If It Crashes")
            }
            Section {
                LabeledContent("Version", value: Self.versionText)
            } footer: {
                Text("Mention this version if you write to us another way.")
            }
        }
        .navigationTitle("Feedback & Bugs")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func step(_ number: Int, _ text: LocalizedStringKey) -> some View {
        DSIconRow(systemImage: "\(number).circle.fill") {
            Text(text).fixedSize(horizontal: false, vertical: true)
        }
    }

    /// Opens the TestFlight app.
    static let testFlightURL = URL(string: "itms-beta://")

    static var versionText: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "?"
        let build = info?["CFBundleVersion"] as? String ?? "?"
        return "\(version) (\(build))"
    }
}

/// The row that opens `FeedbackView`, for the Household tab and the setup screen. Only in
/// development builds and, in TestFlight, for households that tested the beta: the steps are
/// TestFlight's, and App Review sees a sandbox build too.
struct FeedbackLink: View {
    private let builds = BuildChannelStore.shared
    @Environment(PlusStore.self) private var plus: PlusStore?

    var body: some View {
        if builds.channel?.showsBetaFeedback(testedTheBeta: plus?.status?.comp?.isBeta == true) == true {
            NavigationLink {
                FeedbackView()
            } label: {
                DSIconRow(systemImage: "bubble.left.and.exclamationmark.bubble.right") {
                    Text("Feedback & Bugs")
                }
            }
        }
    }
}

/// "Contact Us" on the Household tab, in every build: the website's support page, which
/// has our email. Apple asks apps where people share content to say how to reach us.
struct ContactUsLink: View {
    static let supportURL = URL(string: "https://dinneros.app/support/")

    var body: some View {
        if let url = Self.supportURL {
            Link(destination: url) {
                DSIconRow(systemImage: "envelope") { Text("Contact Us") }
            }
            .accessibilityIdentifier("household.contactUs")
        }
    }
}

#Preview("Feedback") {
    NavigationStack { FeedbackView() }
}
