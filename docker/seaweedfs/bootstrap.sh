#!/bin/sh
# Dựng bucket cho SeaweedFS dev (kế hoạch M1, Task M1-40). Chạy bằng danh tính crm-admin, chạy nhiều lần không lỗi.
# Chạy trong image SeaweedFS (có curl hỗ trợ --aws-sigv4), qua dịch vụ s3-bootstrap của docker/compose.dev.yml:
#   bootstrap.sh main    <endpoint> <tệp cấu hình>   kho chính: <tiền tố>tmp, …public, …originals, …private, …exports
#   bootstrap.sh offsite <endpoint> <tệp cấu hình>   kho ngoài giả: <tiền tố>public, …originals, …private
# CONSOLE_ORIGINS (phân cách bằng dấu phẩy): gốc được phép PUT thẳng vào crm-tmp bằng đường dẫn ký sẵn.
# S3_BUCKET_PREFIX (kho chính) và FILESYNC_DST_BUCKET_PREFIX (kho ngoài giả), mặc định crm-: tiền tố tên bucket, cùng
# giá trị với ứng dụng và filesync (Task 410, review độc lập M1 C-06).
# WEED_MASTER (mặc định localhost:9333, tức chạy trong chính container SeaweedFS): master cho weed shell, để bật
# lifecycle TTL fast path của crm-tmp (Task 410, C-08: `weed server` không chạy lifecycle worker; không có fast path thì
# quy tắc hết hạn 1 ngày không xóa gì, đo 19/11/2026 ở docs/do-luong/seaweedfs-m1.md).
# <tiền tố>exports (Task 515, kế hoạch M6 G5): tệp xuất báo cáo, danh sách khách; không giữ phiên bản, không chép ra kho
# ngoài (filesync chỉ chép public, originals, private), tự xóa theo tiền tố khóa: reports/ 7 ngày, customers/ 1 ngày
# (fast path gắn ttlSec theo luật khớp tiền tố, đo 27/11/2026).
#
# Quyền xóa (ADR-36): quyền "Write" của SeaweedFS gồm cả xóa vĩnh viễn một phiên bản, tắt giữ phiên bản và đặt quy
# tắc hết hạn (đo ngày 18/11/2026, docs/do-luong/seaweedfs-m1.md), nên mỗi bucket có chính sách từ chối các việc đó
# với danh tính ứng dụng. Principal phải ghi dạng arn:aws:iam::000000000000:user/<tên>: dạng khác SeaweedFS bỏ qua.
set -eu

role=${1:?main|offsite}
endpoint=${2:?endpoint}
config=${3:?tệp cấu hình s3}
origins=${CONSOLE_ORIGINS:-http://localhost:4192}
master=${WEED_MASTER:-localhost:9333}
case "$role" in
  main) p=${S3_BUCKET_PREFIX:-crm-} ;;
  *) p=${FILESYNC_DST_BUCKET_PREFIX:-crm-} ;;
esac

# Khóa của crm-admin: dòng "credentials" ngay sau dòng "name": "crm-admin" (giữ đúng dạng đó trong tệp cấu hình).
cred=$(awk '/"name": *"crm-admin"/ {getline; print; exit}' "$config")
ak=$(printf '%s' "$cred" | sed -n 's/.*"accessKey": *"\([^"]*\)".*/\1/p')
sk=$(printf '%s' "$cred" | sed -n 's/.*"secretKey": *"\([^"]*\)".*/\1/p')
[ -n "$ak" ] && [ -n "$sk" ] || { echo "bootstrap: không đọc được khóa crm-admin trong $config" >&2; exit 1; }

# s3 <phương thức> <đường dẫn> [thân] — gọi S3 có ký SigV4, in mã HTTP; thân XML/JSON kèm Content-MD5.
s3() {
  if [ $# -ge 3 ]; then
    hex=$(printf '%s' "$3" | md5sum | cut -c1-32)
    md5=$(printf "$(printf '%s' "$hex" | sed 's/../\\x&/g')" | base64)
    curl -sS -o /tmp/s3-body -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" -u "$ak:$sk" \
      -X "$1" "$endpoint$2" -H "Content-MD5: $md5" --data-binary "$3"
  else
    curl -sS -o /tmp/s3-body -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" -u "$ak:$sk" -X "$1" "$endpoint$2"
  fi
}

# must <mô tả> <mã chấp nhận…> -- <lệnh s3…>
must() {
  what=$1; shift; ok=""
  while [ "$1" != "--" ]; do ok="$ok $1"; shift; done; shift
  code=$(s3 "$@")
  case " $ok " in
    *" $code "*) echo "bootstrap: $what: $code" ;;
    *) echo "bootstrap: $what: HTTP $code" >&2; cat /tmp/s3-body >&2; echo >&2; exit 1 ;;
  esac
}

i=0
until curl -s -o /dev/null "$endpoint/"; do
  i=$((i + 1)); [ "$i" -le 60 ] || { echo "bootstrap: $endpoint không trả lời sau 60 s" >&2; exit 1; }
  sleep 1
done

# policy <bucket> <danh tính> <việc…>: in chính sách bucket từ chối các việc liệt kê với danh tính đó.
policy() {
  b=$1 who=$2; shift 2
  acts=$(printf '"s3:%s",' "$@"); acts=${acts%,}
  printf '{"Version":"2012-10-17","Statement":[{"Sid":"KhongXoaVinhVien","Effect":"Deny","Principal":{"AWS":["arn:aws:iam::000000000000:user/%s"]},"Action":[%s],"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}]}' \
    "$who" "$acts" "$b" "$b"
}

# Việc làm mất bản cũ hay nới cấu hình bucket; danh tính ứng dụng không bao giờ cần. Tên việc phân biệt hoa thường
# (SeaweedFS 4.48 nhận "PutBucketCors", bỏ qua "PutBucketCORS"); mỗi tên có ca kiểm trong tests/probe.
loosen="DeleteObjectVersion PutBucketVersioning PutLifecycleConfiguration PutBucketPolicy DeleteBucketPolicy PutBucketCors DeleteBucketCors DeleteBucket"
# fastpath <bucket>: bật lifecycle TTL fast path. PutObject gắn ttlSec theo luật Expiration khớp khóa và ghi vào volume
# có Ttl; filer và volume server tự xóa khi hết hạn, không cần tiến trình nào khác. Chỉ có tác dụng với đối tượng ghi
# SAU lệnh này, chỉ cho bucket không giữ phiên bản.
fastpath() {
  fp=$(printf 's3.bucket.lifecycle.fastpath -name %s -enable\ns3.bucket.lifecycle.fastpath -name %s\n' "$1" "$1" |
    timeout 60 weed shell -master="$master" 2>&1) || true
  case "$fp" in
    *"fast path: enabled"*) echo "bootstrap: hạn TTL fast path $1: bật" ;;
    *) echo "bootstrap: không bật được TTL fast path của $1 (master $master):" >&2; echo "$fp" >&2; exit 1 ;;
  esac
}

versioning='<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>'

case "$role" in
  main)
    for b in "${p}tmp" "${p}public" "${p}originals" "${p}private"; do
      must "tạo $b" 200 409 -- PUT "/$b"
    done
    for b in "${p}public" "${p}originals" "${p}private"; do
      must "giữ phiên bản $b" 200 -- PUT "/$b?versioning" "$versioning"
    done
    for b in "${p}tmp" "${p}public" "${p}originals" "${p}private"; do
      # shellcheck disable=SC2086
      must "chính sách $b" 200 204 -- PUT "/$b?policy" "$(policy "$b" crm-app $loosen)"
    done
    must "hạn 1 ngày ${p}tmp" 200 -- PUT "/${p}tmp?lifecycle" \
      '<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ID>tmp-1-ngay</ID><Filter><Prefix></Prefix></Filter><Status>Enabled</Status><Expiration><Days>1</Days></Expiration><AbortIncompleteMultipartUpload><DaysAfterInitiation>1</DaysAfterInitiation></AbortIncompleteMultipartUpload></Rule></LifecycleConfiguration>'
    rules=""
    for o in $(printf '%s' "$origins" | tr ',' ' '); do rules="$rules<AllowedOrigin>$o</AllowedOrigin>"; done
    must "CORS ${p}tmp" 200 -- PUT "/${p}tmp?cors" \
      "<CORSConfiguration xmlns=\"http://s3.amazonaws.com/doc/2006-03-01/\"><CORSRule>$rules<AllowedMethod>PUT</AllowedMethod><AllowedHeader>*</AllowedHeader><ExposeHeader>ETag</ExposeHeader><MaxAgeSeconds>3000</MaxAgeSeconds></CORSRule></CORSConfiguration>"
    # Fast path của crm-tmp: ttlSec 86400, volume Ttl:1d (R10: ảnh thô còn EXIF/GPS không nằm lại).
    fastpath "${p}tmp"
    # Tệp xuất: không bật giữ phiên bản (fast path không áp cho bucket giữ phiên bản). crm-app không xóa được gì, kể cả
    # DeleteObject (bucket không phiên bản: xóa là mất hẳn, ADR-36); chỉ luật hết hạn dọn.
    must "tạo ${p}exports" 200 409 -- PUT "/${p}exports"
    # shellcheck disable=SC2086
    must "chính sách ${p}exports" 200 204 -- PUT "/${p}exports?policy" "$(policy "${p}exports" crm-app DeleteObject $loosen)"
    must "hạn theo tiền tố ${p}exports" 200 -- PUT "/${p}exports?lifecycle" \
      '<LifecycleConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Rule><ID>reports-7-ngay</ID><Filter><Prefix>reports/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>7</Days></Expiration></Rule><Rule><ID>customers-1-ngay</ID><Filter><Prefix>customers/</Prefix></Filter><Status>Enabled</Status><Expiration><Days>1</Days></Expiration></Rule></LifecycleConfiguration>'
    fastpath "${p}exports"
    ;;
  offsite)
    for b in "${p}public" "${p}originals" "${p}private"; do
      must "tạo $b" 200 409 -- PUT "/$b"
      must "giữ phiên bản $b" 200 -- PUT "/$b?versioning" "$versioning"
      # Kho ngoài chỉ chép thêm: crm-filesync không xóa gì, kể cả dấu xóa.
      # shellcheck disable=SC2086
      must "chính sách $b" 200 204 -- PUT "/$b?policy" "$(policy "$b" crm-filesync DeleteObject $loosen)"
    done
    ;;
  *) echo "bootstrap: vai trò '$role' không biết (main|offsite)" >&2; exit 2 ;;
esac
echo "bootstrap: $role xong"
