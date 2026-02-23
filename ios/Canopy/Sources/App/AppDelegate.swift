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
///
/// Also manages:
/// - App badge count (attention sessions)
/// - Deep link routing from notification taps (canopy://session/{id})
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

    func applicationDidBecomeActive(_ application: UIApplication) {
        // Clear badge when the user opens the app
        application.applicationIconBadgeNumber = 0
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
            // Update badge to reflect current attention count
            updateBadgeCount()

            pushService?.handleBackgroundNotification(userInfo: userInfo) { success in
                completionHandler(success ? .newData : .noData)
            }
        }
    }

    // MARK: - Deep link handling (canopy://session/{sessionId})

    func application(
        _ application: UIApplication,
        open url: URL,
        options: [UIApplication.OpenURLOptionsKey: Any] = [:]
    ) -> Bool {
        handleDeepLink(url)
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
            // Deep link to the session
            await MainActor.run {
                appState?.pendingDeepLinkSessionId = sessionId
                appState?.selectedSessionId = sessionId
            }

        default:
            break
        }
    }

    // MARK: - Badge

    /// Update the app icon badge to the current attention count from SessionStore.
    func updateBadgeCount() {
        guard let appState else { return }
        let count = appState.sessionStore.attentionCount
        UIApplication.shared.applicationIconBadgeNumber = count
    }

    // MARK: - Deep Links

    /// Parse a canopy:// URL and set the pending navigation in AppState.
    ///
    /// Supported schemes:
    /// - canopy://session/{sessionId}
    @discardableResult
    private func handleDeepLink(_ url: URL) -> Bool {
        guard url.scheme == "canopy" else { return false }

        // canopy://session/{sessionId}
        if url.host == "session",
           let sessionId = url.pathComponents.dropFirst().first, !sessionId.isEmpty {
            Task { @MainActor in
                appState?.pendingDeepLinkSessionId = sessionId
                appState?.selectedSessionId = sessionId
            }
            logger.info("Deep link to session: \(sessionId)")
            return true
        }

        logger.warning("Unrecognized deep link: \(url.absoluteString)")
        return false
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
