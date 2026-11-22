import SwiftUI
import UIKit

/// Shown at launch while sign-in is behind Face ID (or Touch ID, Optic ID). Asks once by itself;
/// "Sign in another way" signs out. Coming back from the background never shows it.
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
                            SignInButtonLabel(title: Text(kind.signInTitle), height: buttonHeight) {
                                Image(systemName: kind.systemImage)
                                    .resizable()
                                    .scaledToFit()
                                    .fontWeight(.medium)
                            }
                        }
                        .buttonStyle(.dsSignInPrimary)
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
    /// The offer after sign-in and the error when turning it on fails.
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
                    "Couldn’t Turn It On",
                    isPresented: Binding(presenting: $lock.turnOnError),
                    presenting: lock.turnOnError
                ) { _ in
                    Button("OK") {}
                } message: { message in
                    Text(message)
                }
        } else {
            content
        }
    }
}
