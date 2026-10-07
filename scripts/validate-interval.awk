# Nonnegative Go-duration preflight: exact integer nanoseconds plus Go's
# leadingFraction/float64 rounding. -v emit_total=1 prints ns for parity tests.
# Run LC_ALL=C: macOS Unicode collation can equate s and microsecond spellings.
function norm(s) { sub(/^0+/, "", s); return s == "" ? "0" : s }
function add(a, b, i, j, carry, n, out) {
    carry=0; out=""; i=length(a); j=length(b)
    while(i>0 || j>0 || carry) {
        n=carry+(i>0 ? substr(a,i,1)+0 : 0)+(j>0 ? substr(b,j,1)+0 : 0)
        out=(n%10) out; carry=int(n/10); i--; j--
    }
    return norm(out)
}
function mul(a, factor, i, carry, n, out) {
    carry=0; out=""
    for(i=length(a); i>0; i--) {
        n=(substr(a,i,1)+0)*factor+carry
        out=(n%10) out; carry=int(n/10)
    }
    while(carry) { out=(carry%10) out; carry=int(carry/10) }
    return norm(out)
}
# Go bounds the fractional numerator at 2^63, ignores excess digits, and
# computes uint64(float64(numerator) * (float64(unit) / scale)).
function go_fraction(digits, factor, numerator, scale, i, candidate) {
    numerator="0"; scale=1
    for(i=1; i<=length(digits); i++) {
        if(length(numerator)>18 || (length(numerator)==18 && ("x" numerator)>"x922337203685477580")) break
        candidate=norm(numerator substr(digits,i,1))
        if(length(candidate)>19 || (length(candidate)==19 && ("x" candidate)>"x9223372036854775808")) break
        numerator=candidate; scale*=10
    }
    return sprintf("%.0f",int((numerator+0)*(factor/scale)))
}
function overflow(s) {
    return length(s)>19 || (length(s)==19 && ("x" s)>"x9223372036854775807")
}
BEGIN {
    rest=ENVIRON["DYNO_CHECK_INTERVAL"]; sub(/^\+/, "", rest)
    if(rest=="0") { if(emit_total) print "0"; exit 0 }
    if(rest=="") exit 1
    split("ns us µs μs ms s m h", units, " ")
    factors[1]=1; factors[2]=1000; factors[3]=1000; factors[4]=1000
    factors[5]=1000000; factors[6]=1000000000
    factors[7]=60000000000; factors[8]=3600000000000
    total="0"
    while(rest!="") {
        if(!match(rest, /^([0-9]+(\.[0-9]*)?|\.[0-9]+)/)) exit 1
        number=substr(rest,1,RLENGTH); rest=substr(rest,RLENGTH+1); factor=0
        for(k=1; k<=8; k++) {
            if(substr(rest,1,length(units[k]))==units[k]) {
                factor=factors[k]; rest=substr(rest,length(units[k])+1); break
            }
        }
        if(!factor) exit 1
        split(number,parts,"."); whole=mul(norm(parts[1]),factor)
        fraction=go_fraction(parts[2],factor)
        total=add(total,add(whole,fraction)); if(overflow(total)) exit 1
    }
    if(emit_total) print total
}
