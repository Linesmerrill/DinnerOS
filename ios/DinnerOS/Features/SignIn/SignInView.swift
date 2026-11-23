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
    @Environment(AuthSession.self) private var session
    /// `nil` in previews.
    @Environment(BiometricLock.self) private var lock: BiometricLock?

    @Environment(\.appConfiguration) private var configuration
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.scenePhase) private var scenePhase
    @ScaledMetric(relativeTo: .body) private var buttonHeight: CGFloat = DS.ButtonSize.large.height
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 72

    init(model: SignInModel) {
        _model = State(initialValue: model)
    }

    var body: some View {
        GeometryReader { proxy in
            // A short screen (iPhone SE, the Duo's cover screen) keeps everything, down to the
            // Privacy Policy link, on screen without scrolling (build 121).
            let compact = proxy.size.height < 760
            ScrollView {
                VStack(spacing: compact ? DS.Space.l : DS.Space.xxxl) {
                    Spacer(minLength: 0)
                    header(compact: compact)
                    Spacer(minLength: 0)
                    actions
                }
                .padding(.horizontal, DS.Space.xxl)
                .padding(.vertical, compact ? DS.Space.s : DS.Space.l)
                .frame(maxWidth: 480)
                .frame(maxWidth: .infinity, minHeight: proxy.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .background(backdrop)
        .task { await model.loadOptions() }
        // Signed out with Face ID sign-in kept: ask once by itself (build 119), once the app is
        // in front (at launch the screen appears before it is).
        .task(id: scenePhase) {
            guard scenePhase == .active else { return }
            await lock?.signInIfReady()
        }
        .onChange(of: model.errorMessage) { _, message in
            if let message {
                AccessibilityNotification.Announcement(message).post()
            }
        }
    }

    private func header(compact: Bool) -> some View {
        VStack(spacing: compact ? DS.Space.s : DS.Space.l) {
            Image(systemName: "fork.knife.circle.fill")
                .font(.system(size: compact ? markSize * 0.7 : markSize))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text(configuration.displayName)
                .font(DS.Typography.largeTitle)
                .accessibilityAddTraits(.isHeader)
            Text("Plan the week’s dinners together, shop once, and cook with less guesswork.")
                .font(compact ? DS.Typography.callout : DS.Typography.subtitle)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var actions: some View {
        VStack(spacing: DS.Space.m) {
            if let message = model.errorMessage {
                errorBanner(message)
            }

            if let notice = session.biometricNotice {
                noticeBanner(notice)
            }

            if let lock, session.canSignInWithBiometrics {
                biometricButton(lock)
            }

            if model.isWorking {
                ProgressView("Signing in…")
                    .padding(.bottom, DS.Space.xxs)
            }

            // A custom Sign in with Apple button within Apple's rules, so all three sign-in
            // buttons share one title font (DS.Typography.signInButton, build 121).
            AppleSignInButton(height: buttonHeight) { request in
                model.prepareAppleRequest(request)
            } completion: { result in
                model.handleAppleCompletion(result)
            }

            if model.isGoogleAvailable {
                GoogleSignInButton {
                    Task { await model.signInWithGoogle() }
                }
            }

            if model.isEmailSignInAvailable {
                EmailSignInSection(model: model)
            }

            Text("Only the people you invite can see your household’s plans.")
                .font(DS.Typography.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, DS.Space.xxs)

            if let privacyURL = configuration.privacyPolicyURL {
                Link("Privacy Policy", destination: privacyURL)
                    .font(DS.Typography.footnote)
            }

            // Below everything a person sees, and only in Debug builds against the development
            // server. Screenshot QA hides it (QA_HIDE_DEVELOPER_SIGN_IN) to show the Release screen.
            #if DEBUG
                if configuration.environment == .development,
                    ProcessInfo.processInfo.environment["QA_HIDE_DEVELOPER_SIGN_IN"] != "1"
                {
                    developerSignIn
                        .padding(.top, DS.Space.l)
                }
            #endif
        }
        .disabled(model.isWorking)
    }

    #if DEBUG
        /// The developer affordance. Only ever mounted from inside `#if DEBUG` and an
        /// `environment == .development` check, so it can't reach a Release build or a
        /// build pointed at production.
        private var developerSignIn: some View {
            VStack(spacing: DS.Space.s) {
                Button {
                    Task { await model.signInForDevelopment() }
                } label: {
                    Label("Developer sign-in as \(model.developerIdentity.displayName)", systemImage: "hammer")
                }
                .buttonStyle(.dsSecondary(fullWidth: true))
                .accessibilityHint("Signs in with a test account on the development server.")

                HStack(spacing: DS.Space.s) {
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
                        Label("Other accounts", systemImage: "person.2")
                            .labelStyle(.iconOnly)
                    }
                    .accessibilityLabel("Choose a test account")
                }
            }
        }
    #endif

    /// The first way to sign in when Face ID sign-in is kept on this device.
    private func biometricButton(_ lock: BiometricLock) -> some View {
        let kind = lock.kind ?? .faceID
        return Button {
            Task { await lock.signIn() }
        } label: {
            SignInButtonLabel(title: Text(kind.signInTitle), height: buttonHeight) {
                Image(systemName: kind.systemImage)
                    .resizable()
                    .scaledToFit()
                    .fontWeight(.medium)
            }
        }
        .buttonStyle(.dsSignInPrimary)
        .disabled(lock.isSigningIn)
        .accessibilityIdentifier("signIn.biometric")
    }

    /// Why Face ID sign-in stopped.
    private func noticeBanner(_ message: String) -> some View {
        Label(message, systemImage: "faceid")
            .font(DS.Typography.callout)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(DS.Space.l)
            .background(DS.Colors.noticeFill, in: .rect(cornerRadius: DS.Radius.medium, style: .continuous))
            .accessibilityIdentifier("signIn.biometricNotice")
    }

    private func errorBanner(_ message: String) -> some View {
        VStack(spacing: DS.Space.m) {
            Label(message, systemImage: "exclamationmark.triangle.fill")
                .font(DS.Typography.callout)
                .foregroundStyle(.primary)
                .symbolRenderingMode(.multicolor)
                .frame(maxWidth: .infinity, alignment: .leading)
            if model.canRetry {
                Button("Try Again") {
                    Task { await model.retry() }
                }
                .buttonStyle(.dsSecondary(.small))
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(DS.Space.l)
        .background(DS.Colors.errorFill, in: .rect(cornerRadius: DS.Radius.medium, style: .continuous))
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
