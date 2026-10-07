# Byte-oriented UTF-8 validation; run with LC_ALL=C. Never interprets text as awk
# -v escapes. Printable ranges are synchronized with Go by installer tests.
function printable(cp, low, high, mid) {
    low=1; high=rangeCount
    while(low<=high) {
        mid=int((low+high)/2)
        if(cp<lo[mid]) high=mid-1
        else if(cp>hi[mid]) low=mid+1
        else return 1
    }
    return 0
}
BEGIN {
    while((getline line < ARGV[1])>0) {
        if(line ~ /^#/) continue
        split(line, bounds, " ")
        lo[++rangeCount]=bounds[1]+0; hi[rangeCount]=bounds[2]+0
    }
    close(ARGV[1]); delete ARGV[1]
    for(i=1; i<256; i++) ord[sprintf("%c",i)]=i
    text=ENVIRON["DYNO_CHECK_TEXT"]; count=0
    for(i=1; i<=length(text);) {
        byte=ord[substr(text,i++,1)]; cp=byte; continuation=0; minimum=0
        if(byte>=194 && byte<=223) { cp=byte-192; continuation=1; minimum=128 }
        else if(byte>=224 && byte<=239) { cp=byte-224; continuation=2; minimum=2048 }
        else if(byte>=240 && byte<=244) { cp=byte-240; continuation=3; minimum=65536 }
        else if(byte>=128) exit 1
        for(j=0; j<continuation; j++) {
            if(i>length(text)) exit 1
            byte=ord[substr(text,i++,1)]
            if(byte<128 || byte>191) exit 1
            cp=cp*64+byte-128
        }
        # Reject overlong encodings, surrogates (not printable), and > U+10FFFF.
        if(cp<minimum || cp>1114111 || !printable(cp)) exit 1
        count++
    }
    print count
}
