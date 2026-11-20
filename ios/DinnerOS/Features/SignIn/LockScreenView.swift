import SwiftUI
import UIKit

/// Shown while sign-in is behind Face ID (or Touch ID, Optic ID): at launch, and after more
/// than 15 minutes away. Asks once by itself; "Sign in another way" signs out.
struct LockScreenView: View {
    @Environment(BiometricLock.self) private var lock
    @Environment(\.appConfiguration) private var configuration
    @Environment(\.colorScheme) private var colorScheme
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 72
    @ScaledMetric(relativeTo: .body) private var buttonHeight: CGFloat = DS.ButtonSize.large.height

    var body: some View {
        let kind = lock.kind ?? .faceID
        let isAsking = lock.machine.phase == .asking
        GeometryReader { proxy in
            ScrollView {
                VStack(spacing: DS.Space.xxxl) {
                    Spacer(minLength: DS.Space.l)
                    VStack(spacing: DS.Space.l) {
                        Image(systemName: "fork.knife.circle.fill")
                            .font(.system(size: markSize))
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                        Text(configuration.displayName)
                            .font(.largeTitle.bold())
                            .accessibilityAddTraits(.isHeader)
                        Text("Welcome back.")
                            .font(.title3)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: DS.Space.l)
                    VStack(spacing: DS.Space.m) {
                        Button {
                            Task { await lock.ask() }
                        } label: {
                            SignInButtonLabel(title: Text(kind.toggleTitle), height: buttonHeight) {
                                Image(systemName: kind.systemImage)
                                    .resizable()
                                    .scaledToFit()
                                    .fontWeight(.medium)
                            }
                        }
                        .buttonStyle(
                            .dsPrimary(fullWidth: true, font: DS.Typography.signInButton(height: buttonHeight))
                        )
                        .accessibilityIdentifier("lock.unlock")
                        Button("Sign in another way") {
                            Task { await lock.signInAnotherWay() }
                        }
                        .buttonStyle(.dsTextLink)
                        .accessibilityIdentifier("lock.anotherWay")
                    }
                    .disabled(isAsking)
                }
                .padding(.horizontal, DS.Space.xxl)
                .padding(.vertical, DS.Space.l)
                .frame(maxWidth: 480)
                .frame(maxWidth: .infinity, minHeight: proxy.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .background {
            LinearGradient(
                colors: [Color.accentColor.opacity(colorScheme == .dark ? 0.25 : 0.15), DS.Colors.background],
                startPoint: .top, endPoint: .center
            )
            .background(DS.Colors.background)
            .ignoresSafeArea()
        }
        .task { await lock.askIfReady() }
    }
}

extension View {
    /// The offer after sign-in, the error when turning it on fails, and the lock over the app
    /// after 15 minutes away. The lock is its own window, so it covers sheets too.
    func biometricSignIn(_ lock: BiometricLock?, configuration: AppConfiguration) -> some View {
        modifier(BiometricSignInModifier(lock: lock, configuration: configuration))
    }
}

private struct BiometricSignInModifier: ViewModifier {
    let lock: BiometricLock?
    let configuration: AppConfiguration

    func body(content: Content) -> some View {
        if let lock {
            @Bindable var lock = lock
            let kind = lock.kind ?? .faceID
            content
                .alert(kind.offerTitle, isPresented: $lock.isOffering) {
                    Button("Use \(kind.name)") { Task { await lock.turnOn() } }
                    Button("Not Now", role: .cancel) {}
                }
                .alert(
                    "Couldn't Turn It On",
                    isPresented: Binding(presenting: $lock.turnOnError),
                    presenting: lock.turnOnError
                ) { _ in
                    Button("OK") {}
                } message: { message in
                    Text(message)
                }
                .onChange(of: lock.coversSignedInApp, initial: true) { _, covers in
                    LockWindow.shared.show(covers, lock: lock, configuration: configuration)
                }
        } else {
            content
        }
    }
}

/// A window above everything, sheets and alerts included, while the lock covers the app.
@MainActor
final class LockWindow {
    static let shared = LockWindow()
    private var window: UIWindow?

    func show(_ shown: Bool, lock: BiometricLock, configuration: AppConfiguration) {
        guard shown else {
            window?.isHidden = true
            window = nil
            return
        }
        guard window == nil,
            let scene = UIApplication.shared.connectedScenes
                .compactMap({ $0 as? UIWindowScene })
                .first(where: { $0.activationState != .unattached })
        else { return }
        let window = UIWindow(windowScene: scene)
        window.windowLevel = .alert + 1
        window.rootViewController = UIHostingController(
            rootView: LockScreenView()
                .environment(lock)
                .environment(\.appConfiguration, configuration))
        window.makeKeyAndVisible()
        self.window = window
    }
}
