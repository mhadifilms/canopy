#if os(iOS)
import Foundation
import UserNotifications
import OSLog

private let logger = Logger(subsystem: "dev.canopy.app", category: "PushNotificationService")

/// Manages push notification registration, categories, and handling.
///
/// Notification categories per PLAN.md section 3.9.3:
/// - APPROVAL_REQUEST: Approve/Reject actions (authentication required)
/// - SESSION_ALERT: Open action (launches app to foreground)
///
/// Lock screen Approve/Reject actions trigger ~30s background execution
/// to send "y\n" or "n\n" through the WireGuard tunnel.
@MainActor
@Observable
final class PushNotificationService: NSObject, Sendable {

    /// The current APNs device token, hex-encoded.
    private(set) var apnsToken: String?

    /// Whether the user has granted notification permissions.
    private(set) var isAuthorized: Bool = false

    /// Callback when an approval action is received from a notification.
    var onApprovalAction: (@Sendable (_ sessionId: String, _ approved: Bool, _ macDeviceId: String) async -> Void)?

    /// Callback when a session alert "Open" action is received.
    var onOpenSession: (@Sendable (_ sessionId: String, _ macDeviceId: String) -> Void)?

    // MARK: - Category Identifiers

    static let approvalRequestCategory = "APPROVAL_REQUEST"
    static let sessionAlertCategory = "SESSION_ALERT"

    // MARK: - Action Identifiers

    static let approveAction = "APPROVE_ACTION"
    static let rejectAction = "REJECT_ACTION"
    static let openAction = "OPEN_ACTION"

    // MARK: - Setup

    /// Register notification categories and request authorization.
    func setup() async {
        registerCategories()
        await requestAuthorization()
    }

    /// Register the notification action categories with iOS.
    private func registerCategories() {
        // APPROVAL_REQUEST: Approve + Reject, both require authentication
        let approve = UNNotificationAction(
            identifier: Self.approveAction,
            title: "Approve",
            options: [.authenticationRequired]
        )

        let reject = UNNotificationAction(
            identifier: Self.rejectAction,
            title: "Reject",
            options: [.authenticationRequired, .destructive]
        )

        let approvalCategory = UNNotificationCategory(
            identifier: Self.approvalRequestCategory,
            actions: [approve, reject],
            intentIdentifiers: [],
            options: []
        )

        // SESSION_ALERT: Open action (brings app to foreground)
        let open = UNNotificationAction(
            identifier: Self.openAction,
            title: "Open",
            options: [.foreground]
        )

        let sessionAlertCategory = UNNotificationCategory(
            identifier: Self.sessionAlertCategory,
            actions: [open],
            intentIdentifiers: [],
            options: []
        )

        UNUserNotificationCenter.current().setNotificationCategories([
            approvalCategory,
            sessionAlertCategory,
        ])

        logger.info("Notification categories registered")
    }

    /// Request notification authorization from the user.
    private func requestAuthorization() async {
        do {
            let granted = try await UNUserNotificationCenter.current()
                .requestAuthorization(options: [.alert, .sound, .badge])
            isAuthorized = granted

            if granted {
                logger.info("Notification authorization granted")
            } else {
                logger.warning("Notification authorization denied")
            }
        } catch {
            logger.error("Failed to request notification authorization: \(error)")
        }
    }

    // MARK: - Token Handling

    /// Called when APNs provides a device token (from AppDelegate).
    func didRegisterForRemoteNotifications(deviceToken: Data) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        apnsToken = token
        logger.info("APNs token received: \(token.prefix(8))...")
    }

    /// Called when APNs registration fails.
    func didFailToRegisterForRemoteNotifications(error: Error) {
        logger.error("APNs registration failed: \(error)")
        apnsToken = nil
    }

    // MARK: - Notification Response Handling

    /// Handle a notification response (user tapped an action or the notification itself).
    ///
    /// For APPROVAL_REQUEST actions, triggers background execution to send
    /// approve/reject through the tunnel (~30s background execution window).
    func handleNotificationResponse(_ response: UNNotificationResponse) async {
        let userInfo = response.notification.request.content.userInfo
        let categoryId = response.notification.request.content.categoryIdentifier

        guard let sessionId = userInfo["session_id"] as? String,
              let macDeviceId = userInfo["mac_device_id"] as? String else {
            logger.warning("Notification missing session_id or mac_device_id in userInfo")
            return
        }

        switch categoryId {
        case Self.approvalRequestCategory:
            switch response.actionIdentifier {
            case Self.approveAction:
                logger.info("User approved action for session \(sessionId)")
                await onApprovalAction?(sessionId, true, macDeviceId)

            case Self.rejectAction:
                logger.info("User rejected action for session \(sessionId)")
                await onApprovalAction?(sessionId, false, macDeviceId)

            case UNNotificationDefaultActionIdentifier:
                // User tapped the notification itself - open the session
                onOpenSession?(sessionId, macDeviceId)

            default:
                break
            }

        case Self.sessionAlertCategory:
            switch response.actionIdentifier {
            case Self.openAction, UNNotificationDefaultActionIdentifier:
                onOpenSession?(sessionId, macDeviceId)
            default:
                break
            }

        default:
            // Default tap - open the session
            onOpenSession?(sessionId, macDeviceId)
        }
    }

    /// Handle a silent/background push notification.
    ///
    /// Called by AppDelegate's `didReceiveRemoteNotification` for content-available pushes.
    func handleBackgroundNotification(
        userInfo: [AnyHashable: Any],
        completionHandler: @escaping (Bool) -> Void
    ) {
        guard let eventType = userInfo["event_type"] as? String else {
            completionHandler(false)
            return
        }

        logger.info("Background notification received: \(eventType)")

        // Background notifications can pre-warm data
        // The actual approval action is handled via notification actions
        completionHandler(true)
    }
}
#endif
