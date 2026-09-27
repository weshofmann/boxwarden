//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

static NSPasteboard *board(const char *name) {
    if (name[0] == '\0') return [NSPasteboard generalPasteboard];
    NSString *value = [NSString stringWithUTF8String:name];
    return value ? [NSPasteboard pasteboardWithName:value] : nil;
}
int bw_clipboard_read(const char *name, void **result, size_t *length) {
    @autoreleasepool {
        NSPasteboard *pb = board(name);
        if (!pb || ![[pb types] containsObject:NSPasteboardTypeString]) return 2;
        NSData *data = [pb dataForType:NSPasteboardTypeString];
        if (!data) return 2;
        if ([data length] > 1048576) return 1;
        *length = [data length];
        *result = malloc(*length ? *length : 1);
        if (!*result) return 2;
        if (*length) memcpy(*result, [data bytes], *length);
        return 0;
    }
}
typedef struct { NSPasteboard *pb; NSArray *items; } Prepared;
void *bw_clipboard_prepare(const char *name, const void *bytes, size_t length) {
    @autoreleasepool {
        NSData *data = [NSData dataWithBytes:bytes length:length];
        NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
        if (!data || !item || ![item setData:data forType:NSPasteboardTypeString]) {
            [item release]; return NULL;
        }
        NSPasteboard *pb = board(name);
        Prepared *prepared = pb ? calloc(1, sizeof(Prepared)) : NULL;
        if (prepared) {
            prepared->pb = [pb retain];
            prepared->items = [[NSArray alloc] initWithObjects:item, nil];
        }
        [item release];
        return prepared;
    }
}
int bw_clipboard_commit(void *value) {
    @autoreleasepool {
        Prepared *prepared = value;
        // No mutation until the complete item exists and the Go caller rechecks
        // cancellation. Every failure after clearContents has unknown outcome.
        [prepared->pb clearContents];
        return [prepared->pb writeObjects:prepared->items] ? 0 : 2;
    }
}
void bw_clipboard_discard(void *value) {
    @autoreleasepool {
        Prepared *prepared = value;
        [prepared->items release]; [prepared->pb release]; free(prepared);
    }
}
void bw_clipboard_release(const char *name) {
    @autoreleasepool { if (name[0] != '\0') [board(name) releaseGlobally]; }
}
