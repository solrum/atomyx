#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

/// Keys into `XCUIElementSnapshot.additionalAttributes` for the
/// "is visible" and "is accessibility element" flags. The keys are
/// `NSNumber*` integer IDs that the XCTest daemon assigns at
/// runtime by hashing internal accessibility attribute strings —
/// the IDs are NOT stable across Xcode releases, which is why we
/// resolve them at process load time via private symbols rather
/// than hard-coding integers.
///
/// Both expressions return `nil` if the symbol resolution failed
/// (Xcode renamed / removed the symbols on a future release).
/// Callers must null-check before subscripting `additionalAttributes`.
extern NSNumber *_Nullable AtomyxXCAXAIsVisibleAttribute(void);
extern NSNumber *_Nullable AtomyxXCAXAIsElementAttribute(void);

NS_ASSUME_NONNULL_END
