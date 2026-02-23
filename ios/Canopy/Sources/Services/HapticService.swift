#if os(iOS)
import UIKit

/// Centralized haptic feedback utility.
///
/// Wraps UIImpactFeedbackGenerator and UINotificationFeedbackGenerator
/// with a simple static API. All methods are safe to call from any actor;
/// generator work is dispatched to the main actor.
@MainActor
enum HapticService {

    // MARK: - Impact

    static func light() {
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
    }

    static func medium() {
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
    }

    static func heavy() {
        UIImpactFeedbackGenerator(style: .heavy).impactOccurred()
    }

    // MARK: - Notification

    static func success() {
        UINotificationFeedbackGenerator().notificationOccurred(.success)
    }

    static func warning() {
        UINotificationFeedbackGenerator().notificationOccurred(.warning)
    }

    static func error() {
        UINotificationFeedbackGenerator().notificationOccurred(.error)
    }
}
#endif
