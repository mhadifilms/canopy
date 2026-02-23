#if os(iOS)
import Foundation
import UIKit
import UserNotifications
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "AppDelegate")

/// UIKit app delegate for handling push notifications and background execution.
///
/// SwiftUI lifecycle delegates push registration and notification handling here.
/// Coordinates with PushNotificationService for token management and
/// notification action handling (approve/reject from lock screen).
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {

    /// Reference to the push notification service, set by CanopyApp on launch.
    var pushService: PushNotificationService?

    /// Reference to the app state for sending approve/reject through the tunnel.
    var appState: AppState?

    // MARK: - UIApplicationDelegate

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        Task { @MainActor in
            pushService?.didRegisterForRemoteNotifications(deviceToken: deviceToken)
        }
    }

    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        Task { @MainActor in
            pushService?.didFailToRegisterForRemoteNotifications(error: error)
        }
    }

    /// Handle background push notifications (content-available).
    ///
    /// iOS grants ~30s of background execution time.
    func application(
        _ application: UIApplication,
        didReceiveRemoteNotification userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        Task { @MainActor in
            pushService?.handleBackgroundNotification(userInfo: userInfo) { success in
                completionHandler(success ? .newData : .noData)
            }
        }
    }

    // MARK: - UNUserNotificationCenterDelegate

    /// Called when a notification is delivered while the app is in the foreground.
    /// Show the notification banner even when the app is open.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner, .sound, .badge]
    }

    /// Called when the user interacts with a notification (tap, or action button).
    ///
    /// For APPROVAL_REQUEST actions from the lock screen:
    /// iOS provides ~30s of background execution. We use this to:
    /// 1. Ensure the WireGuard tunnel is up (it should be, via always-on VPN)
    /// 2. Connect WebSocket to the Mac
    /// 3. Send "y\n" (approve) or "n\n" (reject) as raw input
    /// 4. Disconnect
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let userInfo = response.notification.request.content.userInfo
        let categoryId = response.notification.request.content.categoryIdentifier

        logger.info("Notification action: \(response.actionIdentifier) category: \(categoryId)")

        guard let sessionId = userInfo["session_id"] as? String,
              let macDeviceId = userInfo["mac_device_id"] as? String else {
            logger.warning("Notification missing required fields in userInfo")
            return
        }

        switch response.actionIdentifier {
        case PushNotificationService.approveAction:
            await handleApprovalFromNotification(
                sessionId: sessionId,
                macDeviceId: macDeviceId,
                approved: true
            )

        case PushNotificationService.rejectAction:
            await handleApprovalFromNotification(
                sessionId: sessionId,
                macDeviceId: macDeviceId,
                approved: false
            )

        case PushNotificationService.openAction,
             UNNotificationDefaultActionIdentifier:
            // Navigate to the session in the app
            await MainActor.run {
                appState?.selectedSessionId = sessionId
            }

        default:
            break
        }
    }

    // MARK: - Background Approval Handling

    /// Send an approval or rejection from a lock screen notification action.
    ///
    /// This runs within iOS's ~30s background execution window.
    /// The WireGuard tunnel should already be up (always-on VPN).
    private func handleApprovalFromNotification(
        sessionId: String,
        macDeviceId: String,
        approved: Bool
    ) async {
        logger.info("Handling \(approved ? "approval" : "rejection") for session \(sessionId) on \(macDeviceId)")

        await MainActor.run {
            guard let appState else {
                logger.error("AppState not available for background approval")
                return
            }

            Task {
                if approved {
                    await appState.approveAction(for: sessionId)
                } else {
                    await appState.rejectAction(for: sessionId)
                }
                logger.info("Background \(approved ? "approval" : "rejection") sent successfully")
            }
        }
    }
}
#endif
