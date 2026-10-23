//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void ankhSetDockIcon(const void *bytes, int length) {
	NSData *data = [NSData dataWithBytes:bytes length:length];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSImage *img = [[NSImage alloc] initWithData:data];
		if (img != nil) {
			[NSApp setApplicationIconImage:img];
		}
	});
}

// label is strdup'ed by the caller and freed here on the main thread.
static void ankhSetBadge(char *label) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSString *s = label[0] ? [NSString stringWithUTF8String:label] : nil;
		[[NSApp dockTile] setBadgeLabel:s];
		free(label);
	});
}
*/
import "C"

import "unsafe"

// setDockBadge shows label on the Dock icon; "" clears it.
func setDockBadge(label string) {
	C.ankhSetBadge(C.CString(label))
}

// setDockIcon shows our icon in the Dock even when the binary runs outside
// an .app bundle (go run, bin/ankh).
func setDockIcon(png []byte) {
	if len(png) == 0 {
		return
	}
	C.ankhSetDockIcon(unsafe.Pointer(&png[0]), C.int(len(png)))
}
