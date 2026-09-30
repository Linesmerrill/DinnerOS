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
                        Label("Open TestFlight", systemImage: "airplane")
                    }
                }
                Text("In TestFlight, open DinnerOS and tap Send Beta Feedback.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
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
        Label {
            Text(text).fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: "\(number).circle.fill").foregroundStyle(.tint)
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

/// The row that opens `FeedbackView`, for the Household tab and the setup screen.
struct FeedbackLink: View {
    var body: some View {
        NavigationLink {
            FeedbackView()
        } label: {
            Label("Feedback & Bugs", systemImage: "bubble.left.and.exclamationmark.bubble.right")
        }
    }
}

#Preview("Feedback") {
    NavigationStack { FeedbackView() }
}
