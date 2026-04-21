#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

/// Returns a canonical Objective-C NSBlock that the XCTest daemon
/// can invoke without bridging through Swift's
/// @convention(block) thunk.
///
/// Why this exists: on Xcode 16.2 / iOS 18.3, the daemon's reply
/// to `_XCT_synthesizeEvent:completion:` invokes the completion
/// block with a sentinel pointer (observed value 0x1) rather than
/// a real NSError* or nil. A Swift @convention(block) thunk
/// auto-retains the argument before the body runs and crashes in
/// objc_retain / swift_unknownObjectRetain. Building the block in
/// Objective-C lets us ignore the argument entirely.
///
/// Capture rule: the block must NOT close over any Swift values.
/// XPC marshalling calls _Block_copy on our block, and Swift-
/// captured closures break that copy path. Pass everything the
/// completion callback needs as `dispatch_semaphore_t`-style
/// Objective-C parameters; this helper signals the supplied
/// semaphore and nothing else.
NSObject *atomyxMakeSemaphoreSignalingBlock(
    dispatch_semaphore_t _Nonnull semaphore
);

NS_ASSUME_NONNULL_END
