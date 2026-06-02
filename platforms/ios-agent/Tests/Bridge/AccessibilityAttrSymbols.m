#import "AccessibilityAttrSymbols.h"

#import <dlfcn.h>
#import <objc/runtime.h>

/// Bridge to XCTest's private accessibility-attribute registry.
///
/// XCTest stores per-snapshot extras in
/// `XCUIElementSnapshot.additionalAttributes`, a dictionary keyed
/// by integer-IDs that the daemon allocates at startup by hashing
/// internal attribute strings. We need two of those keys —
/// "is visible" and "is element" — to read true accessibility-
/// runtime visibility and a11y-leaf flags out of a snapshot.
///
/// The IDs are not stable across Xcode versions. The string names
/// `XC_kAXXCAttributeIsVisible` and `XC_kAXXCAttributeIsElement`
/// ARE stable (they are exported NSString globals inside XCTest),
/// so we look up the strings via dlsym, then call
/// `XCAXAccessibilityAttributesForStringAttributes` (also in
/// XCTest) to resolve them to the integer keys.
///
/// Why all the indirection: `dlsym` against a UI test bundle's
/// own binary cannot find symbols defined in XCTest.framework —
/// we must dlopen the XCTest binary explicitly. The path is the
/// `XCTestCase` class's bundle executable, which is reachable
/// without hard-coding any framework path.
///
/// Status: the resolver is wired and callers receive valid
/// `NSNumber*` keys. However, `XCUIApplication.snapshot()`
/// (the primitive `XCUIBridge.dumpRawTree` calls today) does
/// NOT pre-fetch these attributes into `additionalAttributes`,
/// so reads of the resolved keys come back empty and callers
/// hit their fallback paths. Switching the dump path to a
/// snapshot variant that requests these attributes (similar to
/// WDA's `fb_standardSnapshot` pulling them via XCAXClient_iOS)
/// will make this resolver active without any caller change.

typedef NSArray<NSNumber *> *(*XCAXAttrsFn)(NSArray<NSString *> *);

static NSNumber *gIsVisibleAttribute;
static NSNumber *gIsElementAttribute;

static void *AtomyxRetrieveXCTestSymbol(const char *name) {
    Class xctestClass = objc_lookUpClass("XCTestCase");
    if (xctestClass == Nil) return NULL;
    NSString *binaryPath = [NSBundle bundleForClass:xctestClass].executablePath;
    if (binaryPath == nil) return NULL;
    void *handle = dlopen(binaryPath.UTF8String, RTLD_LAZY);
    if (handle == NULL) return NULL;
    return dlsym(handle, name);
}

__attribute__((constructor))
static void AtomyxResolveXCAXSymbols(void) {
    void *visibleStringSym = AtomyxRetrieveXCTestSymbol("XC_kAXXCAttributeIsVisible");
    void *elementStringSym = AtomyxRetrieveXCTestSymbol("XC_kAXXCAttributeIsElement");
    void *fnSym = AtomyxRetrieveXCTestSymbol("XCAXAccessibilityAttributesForStringAttributes");
    if (visibleStringSym == NULL || elementStringSym == NULL || fnSym == NULL) {
        // Symbol surface drifted (Xcode update). Caller's
        // null-check on the public accessors handles this — we
        // just don't populate the globals.
        return;
    }
    NSString *visibleString = *(NSString *__autoreleasing *)visibleStringSym;
    NSString *elementString = *(NSString *__autoreleasing *)elementStringSym;
    XCAXAttrsFn fn = (XCAXAttrsFn)fnSym;
    NSArray<NSNumber *> *resolved = fn(@[visibleString, elementString]);
    if (resolved.count >= 2) {
        gIsVisibleAttribute = resolved[0];
        gIsElementAttribute = resolved[1];
    }
}

NSNumber *AtomyxXCAXAIsVisibleAttribute(void) {
    return gIsVisibleAttribute;
}

NSNumber *AtomyxXCAXAIsElementAttribute(void) {
    return gIsElementAttribute;
}
