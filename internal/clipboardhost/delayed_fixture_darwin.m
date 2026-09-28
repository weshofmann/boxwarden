//go:build darwin && cgo

#import <AppKit/AppKit.h>
@interface BoxwardenTestDelayedProvider : NSObject<NSPasteboardItemDataProvider>
@end
@implementation BoxwardenTestDelayedProvider
- (void)pasteboard:(NSPasteboard *)pasteboard item:(NSPasteboardItem *)item provideDataForType:(NSPasteboardType)type {
 [NSThread sleepForTimeInterval:1.0];
 [item setData:[@"synthetic delayed text" dataUsingEncoding:NSUTF8StringEncoding] forType:type];
}
@end
void test_seed_promised(const char *name){
 @autoreleasepool {
 NSPasteboard *pb=[NSPasteboard pasteboardWithName:[NSString stringWithUTF8String:name]];
 BoxwardenTestDelayedProvider *provider=[[BoxwardenTestDelayedProvider alloc] init];
 NSPasteboardItem *item=[[NSPasteboardItem alloc] init];
 [item setDataProvider:provider forTypes:@[NSPasteboardTypeString]];
 [pb clearContents];[pb writeObjects:@[item]];
 }
}
