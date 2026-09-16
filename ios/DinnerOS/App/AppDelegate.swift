import UIKit
import UserNotifications

/// Receives what only an app delegate can: the APNs device token, and the push a member
/// tapped (including the one that launched the app). It owns `AppDependencies` so both are
/// wired before `didFinishLaunching` returns — a tap that launches the app is delivered
/// right after it.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    let dependencies = AppDependencies()

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        let push = dependencies.push
        Task { await push.didRegister(deviceToken: deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        dependencies.push.didFailToRegister(error)
    }

    /// A push while the app is open still shows as a banner, and the bell's count follows.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        await dependencies.push.notificationReceived()
        return [.banner, .list, .sound]
    }

    /// Tapping a push opens what its row on the bell opens.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse
    ) async {
        guard response.actionIdentifier == UNNotificationDefaultActionIdentifier,
            let route = PushRoute(userInfo: response.notification.request.content.userInfo)
        else { return }
        await dependencies.push.open(route)
    }
}
