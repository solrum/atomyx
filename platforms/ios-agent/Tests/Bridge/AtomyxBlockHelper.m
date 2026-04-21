#import "AtomyxBlockHelper.h"

NSObject *atomyxMakeSemaphoreSignalingBlock(
    dispatch_semaphore_t semaphore
) {
    // Capture only ObjC types so XPC's _Block_copy works cleanly
    // when the daemon marshals the block across the connection.
    //
    // `__unsafe_unretained` on the parameter prevents ARC from
    // emitting an objc_retain at block entry. The XPC reply is
    // observed to pass a sentinel pointer (0x1) that is not a
    // real object, so any retain crashes. We never read the
    // argument either way.
    return (NSObject *)[^(__unsafe_unretained id _Nullable _arg) {
        (void)_arg;
        dispatch_semaphore_signal(semaphore);
    } copy];
}
