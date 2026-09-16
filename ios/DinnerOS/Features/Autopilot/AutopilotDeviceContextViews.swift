import SwiftUI
import UIKit

/// Shown once, the first time a member plans with Autopilot, before the system asks for
/// calendar and location access: what's read, and what never leaves the phone.
struct AutopilotContextExplainerView: View {
    let onContinue: () -> Void
    let onNotNow: () -> Void

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    VStack(alignment: .leading, spacing: 8) {
                        Image(systemName: "sparkles")
                            .font(.largeTitle)
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                        Text("Plan Around Your Week")
                            .font(.title.bold())
                        Text(
                            "Autopilot can suggest quicker dinners on busy evenings and cozier ones on cold or rainy days."
                        )
                        .foregroundStyle(.secondary)
                    }
                    point(
                        systemImage: "calendar", title: String(localized: "Your calendar"),
                        detail: String(
                            localized:
                                "Your iPhone works out how free each evening is between 4 and 8 pm. Only “free”, “some”, or “busy” and the free minutes are sent — never event names, times, or who's invited."
                        ))
                    point(
                        systemImage: "cloud.sun", title: String(localized: "The forecast"),
                        detail: String(
                            localized:
                                "Your approximate location gets the forecast from Apple Weather on your iPhone. Only “cold”, “mild”, or “hot” and rain or snow are sent — never where you are."
                        ))
                    point(
                        systemImage: "switch.2", title: String(localized: "Your choice"),
                        detail: String(
                            localized:
                                "Both are optional. Autopilot plans without them, and you can turn either off in Autopilot Preferences."
                        ))
                }
                .padding()
            }
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: 8) {
                    Button(action: onContinue) {
                        Text("Continue")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    Button("Not Now", action: onNotNow)
                        .controlSize(.large)
                }
                .padding()
                .background(.bar)
            }
        }
        .interactiveDismissDisabled()
    }

    private func point(systemImage: String, title: String, detail: String) -> some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: systemImage)
                .font(.title2)
                .foregroundStyle(.tint)
                .frame(minWidth: 32)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(.headline)
                Text(detail)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// Autopilot Preferences' Calendar & Weather section: a switch per signal and what iOS allows.
struct AutopilotDeviceContextSection: View {
    let context: AutopilotDeviceContext
    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Section {
            Toggle(isOn: binding(\.usesCalendar, set: context.setUsesCalendar)) {
                label(
                    title: String(localized: "Use Calendar"), systemImage: "calendar",
                    status: status(context.calendarAuthorization, uses: context.usesCalendar))
            }
            Toggle(isOn: binding(\.usesWeather, set: context.setUsesWeather)) {
                label(
                    title: String(localized: "Use Weather"), systemImage: "cloud.sun",
                    status: status(context.locationAuthorization, uses: context.usesWeather))
            }
            if (context.usesCalendar && context.calendarAuthorization == .denied)
                || (context.usesWeather && context.locationAuthorization == .denied)
            {
                Button("Open Settings") {
                    if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                }
            }
        } header: {
            Text("Calendar & Weather")
        } footer: {
            Text(
                "Worked out on this iPhone. Autopilot receives only how busy each evening is and cold, mild, or hot with rain or snow — never your events or location."
            )
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            if phase == .active { context.refreshAuthorization() }
        }
    }

    private func binding(
        _ keyPath: KeyPath<AutopilotDeviceContext, Bool>, set: @escaping (Bool) async -> Void
    ) -> Binding<Bool> {
        Binding(get: { context[keyPath: keyPath] }, set: { value in Task { await set(value) } })
    }

    private func label(title: String, systemImage: String, status: String) -> some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                Text(status)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: systemImage)
        }
    }

    private func status(_ authorization: DeviceSignalAuthorization, uses: Bool) -> String {
        guard uses else { return String(localized: "Off") }
        switch authorization {
        case .granted: return String(localized: "On")
        case .notDetermined: return String(localized: "Not asked yet")
        case .denied: return String(localized: "Not allowed in iOS Settings")
        }
    }
}

/// Apple Weather's required attribution: its mark and a link to the legal page. Shown wherever
/// forecast-derived suggestions appear.
struct WeatherAttributionView: View {
    @Environment(AutopilotDeviceContext.self) private var context: AutopilotDeviceContext?
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let attribution = context?.weatherAttribution ?? .fallback
        VStack(alignment: .leading, spacing: 4) {
            Text("Suggestions use the forecast for your area.")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack(spacing: 8) {
                if let url = colorScheme == .dark ? attribution.darkMarkURL : attribution.lightMarkURL {
                    AsyncImage(url: url) { image in
                        image.resizable().scaledToFit()
                    } placeholder: {
                        Text(attribution.serviceName).font(.caption)
                    }
                    .frame(height: 14)
                    .accessibilityLabel(attribution.serviceName)
                } else {
                    Text(attribution.serviceName)
                        .font(.caption)
                }
                if let legal = attribution.legalPageURL {
                    Link("Data sources", destination: legal)
                        .font(.caption)
                }
            }
        }
        .task { await context?.loadWeatherAttribution() }
    }
}

#Preview("Explainer") {
    AutopilotContextExplainerView(onContinue: {}, onNotNow: {})
}
