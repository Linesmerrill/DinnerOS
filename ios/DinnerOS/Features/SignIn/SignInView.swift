import AuthenticationServices
import SwiftUI

/// Entry point shown while signed out.
struct SignInView: View {
    @Environment(AuthSession.self) private var session
    @Environment(\.googleSignIn) private var googleSignIn

    var body: some View {
        SignInScreen(model: SignInModel(session: session, google: googleSignIn))
    }
}

private struct SignInScreen: View {
    @State private var model: SignInModel

    @Environment(\.appConfiguration) private var configuration
    @Environment(\.colorScheme) private var colorScheme
    @ScaledMetric(relativeTo: .body) private var buttonHeight: CGFloat = 50
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 72

    init(model: SignInModel) {
        _model = State(initialValue: model)
    }

    var body: some View {
        GeometryReader { proxy in
            ScrollView {
                VStack(spacing: 32) {
                    Spacer(minLength: 16)
                    header
                    Spacer(minLength: 16)
                    actions
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 16)
                .frame(maxWidth: 480)
                .frame(maxWidth: .infinity, minHeight: proxy.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .background(backdrop)
        .onChange(of: model.errorMessage) { _, message in
            if let message {
                AccessibilityNotification.Announcement(message).post()
            }
        }
    }

    private var header: some View {
        VStack(spacing: 16) {
            Image(systemName: "fork.knife.circle.fill")
                .font(.system(size: markSize))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text(configuration.displayName)
                .font(.largeTitle.bold())
                .accessibilityAddTraits(.isHeader)
            Text("Plan the week's dinners together, shop once, and cook with less guesswork.")
                .font(.title3)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
    }

    private var actions: some View {
        VStack(spacing: 12) {
            if let message = model.errorMessage {
                errorBanner(message)
            }

            if model.isWorking {
                ProgressView("Signing in…")
                    .padding(.bottom, 4)
            }

            SignInWithAppleButton(.continue) { request in
                model.prepareAppleRequest(request)
            } onCompletion: { result in
                model.handleAppleCompletion(result)
            }
            .signInWithAppleButtonStyle(colorScheme == .dark ? .white : .black)
            // The button doesn't restyle when the color scheme changes; rebuild it.
            .id(colorScheme)
            .frame(height: buttonHeight)
            .clipShape(.rect(cornerRadius: 12, style: .continuous))

            if model.isGoogleAvailable {
                GoogleSignInButton {
                    Task { await model.signInWithGoogle() }
                }
            }

            #if DEBUG
                if configuration.environment == .development {
                    developerSignIn
                }
            #endif

            Text("Only the people you invite can see your household's plans.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .padding(.top, 8)

            if let privacyURL = configuration.privacyPolicyURL {
                Link("Privacy Policy", destination: privacyURL)
                    .font(.footnote)
            }
        }
        .disabled(model.isWorking)
    }

    #if DEBUG
        /// The developer affordance. Only ever mounted from inside `#if DEBUG` and an
        /// `environment == .development` check, so it can't reach a Release build or a
        /// build pointed at production.
        private var developerSignIn: some View {
            VStack(spacing: 8) {
                Button {
                    Task { await model.signInForDevelopment() }
                } label: {
                    Label("Developer sign-in as \(model.developerIdentity.displayName)", systemImage: "hammer")
                        .frame(maxWidth: .infinity, minHeight: buttonHeight)
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.roundedRectangle(radius: 12))
                .accessibilityHint("Signs in with a test account on the development server.")

                HStack(spacing: 8) {
                    TextField("Subject", text: $model.developerSubject)
                        .textFieldStyle(.roundedBorder)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .submitLabel(.go)
                        .onSubmit { Task { await model.signInForDevelopment() } }
                        .accessibilityLabel("Developer subject")
                    Menu {
                        ForEach(DeveloperIdentity.suggestedSubjects, id: \.self) { subject in
                            Button(subject) { model.developerSubject = subject }
                        }
                    } label: {
                        Label("Test accounts", systemImage: "person.2")
                            .labelStyle(.iconOnly)
                    }
                    .accessibilityLabel("Choose a test account")
                }
            }
        }
    #endif

    private func errorBanner(_ message: String) -> some View {
        VStack(spacing: 12) {
            Label(message, systemImage: "exclamationmark.triangle.fill")
                .font(.callout)
                .foregroundStyle(.primary)
                .symbolRenderingMode(.multicolor)
                .frame(maxWidth: .infinity, alignment: .leading)
            if model.canRetry {
                Button("Try Again") {
                    Task { await model.retry() }
                }
                .buttonStyle(.bordered)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding()
        .background(Color.red.opacity(0.12), in: .rect(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .contain)
    }

    private var backdrop: some View {
        LinearGradient(
            colors: [Color.accentColor.opacity(colorScheme == .dark ? 0.25 : 0.15), Color(.systemBackground)],
            startPoint: .top,
            endPoint: .center
        )
        .ignoresSafeArea()
    }
}

#Preview {
    SignInView()
        .environment(AuthSession.preview(.signedOut))
}
