#!/usr/bin/env python3
"""In ra các gói Go cần test: gói chứa tệp vừa đổi và mọi gói phụ thuộc vào chúng (bắc cầu),
cùng toàn bộ gói của vùng rủi ro cao nếu có tệp đổi nằm trong đó.
Đầu vào: danh sách tệp đổi (đường dẫn tương đối) trên stdin. Đầu ra: mỗi dòng một import path.

Tùy chọn (Task 473, 23/11, chủ shop: máy nóng, "chỉ test các case ảnh hưởng"):
  --base REV   bật xét migration: REV là bản đã test xanh để so (điểm tách nhánh, dấu xanh). Migration MỚI chỉ thêm
               (bảng, cột, index, view, quyền…) → test các gói dùng tới đúng đối tượng schema.tên nó chạm, nối qua
               view, hàm, trigger, policy của mọi migration; migration sửa, xóa tệp cũ, pha "-- contract:", lệnh thu hẹp
               (DROP, RENAME, TRUNCATE, DELETE, ALTER COLUMN, SET NOT NULL, OWNER TO, DO $$…), quyền cả schema cũ, câu
               không nhận ra, đối tượng không ghi schema → thoát 5 (bên gọi chạy toàn bộ). Không có --base: bỏ qua .sql.
  --split      mỗi dòng "race <gói>" (gói có mã viết tay hay test bị sửa trực tiếp, vùng rủi ro cao bị mã viết tay
               chạm: go test -race) hoặc "plain <gói>" (gói chỉ dùng tới phần đổi: go test thường, nhẹ hơn nhiều lần).
               Lưới an toàn: toàn bộ test có -race vẫn chạy trên main sau mỗi đợt gộp (main-full-test của điều phối).
Tệp dữ liệu (không phải .go, hay nằm trong testdata/; Task 478): tệp trong thư mục gói → như mã của gói (lan); tệp
testdata → chỉ test của gói (trừ khi mã chạy nhúng nó); tên tệp nằm trong chuỗi của một _test.go → test của gói đó.
Luôn "plain" trừ khi mã viết tay của gói cũng đổi. Xem khối "Tệp dữ liệu" bên dưới.
Biến AFFECTED_MIGRATIONS_DIR (chỉ cho test): đọc migration từ thư mục khác thay cho <gốc>/migrations.
Mã thoát: 0 được; 3 go list lỗi; 4 thư mục Go không khớp gói; 5 cần chạy toàn bộ test."""
import json, os, re, subprocess, sys, unicodedata

# git trả đường dẫn "Thời" dạng NFD, go list dạng NFC (19/11, Task 401): chuẩn hóa NFC trước khi so, không thì không gói nào khớp.
nfc = lambda x: unicodedata.normalize("NFC", x)

args = sys.argv[1:]
split = "--split" in args
base = None
if "--base" in args:
    i = args.index("--base")
    if i + 1 >= len(args) or not args[i + 1]:
        sys.stderr.write("affected.py: --base cần một bản so\n")
        sys.exit(5)
    base = args[i + 1]

changed = [nfc(l.strip()) for l in sys.stdin if l.strip()]
root = nfc(subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip())


def need_full(reason):
    sys.stderr.write("affected.py: chạy toàn bộ test — " + reason + "\n")
    sys.exit(5)


# ---- Migration (chỉ khi có --base) ----
MIG_RE = re.compile(r"^migrations/\d{4}_[^/]+\.sql$")
# Lệnh thu hẹp hay khó đoán phạm vi: giữ luật cũ (toàn bộ test). "DO $$" viết riêng: sau "$" không có ranh giới từ.
NARROW_RE = re.compile(
    r"\b(DROP|RENAME|TRUNCATE|DELETE\s+FROM|ALTER\s+COLUMN|SET\s+NOT\s+NULL|CREATE\s+EXTENSION|CREATE\s+ROLE"
    r"|ALTER\s+ROLE|ALTER\s+DEFAULT\s+PRIVILEGES|ALTER\s+SCHEMA|SET\s+SCHEMA|OWNER\s+TO|ALTER\s+POLICY"
    r"|ALTER\s+TYPE|ALTER\s+FUNCTION|ALTER\s+VIEW|CALL|COPY)\b|\bDO\s+(?:LANGUAGE\s+\w+\s+)?\$",
    re.I)
# Câu nhận ra được: lệnh (nhóm "kind") và đối tượng chính (nhóm "obj"). Phải khớp NGAY ĐẦU câu, không thì một câu lạ
# (CREATE RULE … DO INSERT INTO s.t) bị nhận nhầm theo mẩu bên trong.
_NAME = r"([A-Za-z_][\w\".]*)"
STMT_RE = re.compile(
    r"\s*(?:"
    r"(?P<create>CREATE\s+(?:OR\s+REPLACE\s+)?(?:TABLE|(?:MATERIALIZED\s+)?VIEW|FUNCTION|TYPE|SEQUENCE)(?:\s+IF\s+NOT\s+EXISTS)?)"
    r"|(?P<alter>ALTER\s+TABLE(?:\s+IF\s+EXISTS)?(?:\s+ONLY)?)"
    r"|(?P<index>CREATE\s+(?:UNIQUE\s+)?INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+NOT\s+EXISTS)?(?:\s+\w+)?\s+ON(?:\s+ONLY)?)"
    r"|(?P<trigger>CREATE\s+(?:OR\s+REPLACE\s+)?TRIGGER\s+\w+\s+(?:BEFORE|AFTER|INSTEAD\s+OF)\s+[\w\s,]+?\s+ON)"
    r"|(?P<policy>CREATE\s+POLICY\s+\w+\s+ON)"
    r"|(?P<grant>(?:GRANT|REVOKE)\s+[\w\s(),]+?\s+ON(?:\s+TABLE|\s+FUNCTION|\s+SEQUENCE|\s+SCHEMA)?)"
    r"|(?P<insert>INSERT\s+INTO)"
    r"|(?P<update>UPDATE(?:\s+ONLY)?)"
    r")\s+" + _NAME, re.I)
# Mọi câu (cả thân hàm) chỉ được ghi bảng có schema: một INSERT/UPDATE không ghi schema trong thân hàm → toàn bộ.
WRITE_RE = re.compile(r"\b(?:INSERT\s+INTO|UPDATE(?:\s+ONLY)?)\s+([A-Za-z_][\w\".]*)\s+(?:SET\b|\(|VALUES|SELECT|AS\b|DEFAULT)", re.I)


def strip_sql(text):
    """Bỏ chú thích -- và /* */, chỉ lấy phần goose Up."""
    m = re.search(r"--\s*\+goose\s+Down", text, re.I)
    if m:
        text = text[:m.start()]
    text = re.sub(r"/\*.*?\*/", " ", text, flags=re.S)
    return "\n".join(l.split("--", 1)[0] for l in text.splitlines())


def statements(sql):
    """Tách câu theo ';' ngoài khối $tag$…$tag$ (thân hàm)."""
    out, cur, i, tag = [], [], 0, None
    dollar = re.compile(r"\$\w*\$")
    while i < len(sql):
        if tag is None:
            m = dollar.match(sql, i)
            if m:
                tag = m.group(0); cur.append(tag); i = m.end(); continue
            if sql[i] == ";":
                out.append("".join(cur)); cur = []; i += 1; continue
        elif sql.startswith(tag, i):
            cur.append(tag); i += len(tag); tag = None; continue
        cur.append(sql[i]); i += 1
    out.append("".join(cur))
    return [s for s in (x.strip() for x in out) if s]


def migration_scope(files):
    """Trả tập đối tượng database (schema.tên) bị chạm, đã nối qua view, hàm, trigger, policy dùng chúng; hoặc need_full."""
    mig_dir = os.environ.get("AFFECTED_MIGRATIONS_DIR") or os.path.join(root, "migrations")
    all_mig = sorted(f for f in os.listdir(mig_dir) if f.endswith(".sql"))
    texts = {f: open(os.path.join(mig_dir, f), encoding="utf-8").read() for f in all_mig}
    schemas = set()
    for t in texts.values():
        schemas |= {m.group(2).lower() for m in re.finditer(r"CREATE\s+SCHEMA(\s+IF\s+NOT\s+EXISTS)?\s+(\w+)", t, re.I)}
    if not schemas:
        need_full("không đọc được schema nào từ migrations/")
    obj_ref = re.compile(r"(?<![\w.])((?:" + "|".join(sorted(schemas)) + r")\.\w+)", re.I)

    def qualified(obj, name):
        obj = obj.replace('"', "").lower()
        if "." not in obj or obj.split(".")[0] not in schemas:
            need_full(f"migration {name}: đối tượng '{obj}' không ghi rõ schema")
        return obj

    touched = set()
    for f in files:
        name = os.path.basename(f)
        if subprocess.run(["git", "cat-file", "-e", f"{base}:{f}"], capture_output=True, cwd=root).returncode == 0:
            need_full(f"migration {name} đã có ở bản so ({base}) mà bị sửa")
        if name not in texts:
            need_full(f"migration {name} bị xóa hay đổi tên")
        raw = texts[name]
        if re.search(r"^--\s*contract:", raw, re.M):
            need_full(f"migration {name} là pha thu hẹp (-- contract:)")
        sql = strip_sql(raw)
        hit = NARROW_RE.search(sql)
        if hit:
            need_full(f"migration {name} có lệnh '{hit.group(0).strip()}'")
        for w in WRITE_RE.finditer(sql):          # cả trong thân hàm, trigger
            qualified(w.group(1), name)
        new_schemas = set()
        for st in statements(sql):
            if re.match(r"(?is)\s*(SET|RESET|SELECT\s+set_config|COMMENT\s+ON)\b", st):
                continue
            ms_schema = re.match(r"(?is)\s*CREATE\s+SCHEMA(?:\s+IF\s+NOT\s+EXISTS)?\s+(\w+)\s*$", st)
            if ms_schema:   # nhóm bảng mới: chưa ai dùng; bảng trong đó được tính ở câu tạo bảng
                sname = ms_schema.group(1).lower()
                if any(re.search(rf"(?i)CREATE\s+SCHEMA(\s+IF\s+NOT\s+EXISTS)?\s+{sname}\b", strip_sql(t))
                       for other, t in texts.items() if other != name):
                    need_full(f"migration {name}: tạo lại schema đã có {sname}")
                new_schemas.add(sname)
                continue
            mg = re.match(r"(?is)\s*GRANT\s+USAGE\s+ON\s+SCHEMA\s+([\w\s,]+?)\s+TO\s+[\w\s,]+$", st)
            if mg:          # quyền dùng schema: chỉ cho schema vừa tạo trong chính migration này
                if not {x.strip().lower() for x in mg.group(1).split(",")} <= new_schemas:
                    need_full(f"migration {name}: quyền trên schema đã có")
                continue
            if re.match(r"(?is)\s*SELECT\s+\w+\.\w+\s*\(", st):   # gọi hàm trợ giúp (vd platform.enable_tenant_rls('s.bảng'))
                args_ = re.findall(r"'([^']*)'", st)   # chỉ đối tượng truyền vào (trong nháy), không tính chính hàm trợ giúp
                objs = {x.lower() for a_ in args_ for x in obj_ref.findall(a_)}
                if not objs:
                    need_full(f"migration {name}: gọi hàm không rõ đối tượng '{st[:60]}…'")
                touched |= objs
                continue
            m = STMT_RE.match(st)
            if not m:
                need_full(f"migration {name}: không nhận ra câu '{st[:60]}…'")
            obj = m.group(m.lastindex)
            if obj.upper() in ("ALL", "SCHEMA", "SEQUENCE", "FUNCTION", "TABLE"):   # GRANT … ON ALL TABLES IN SCHEMA s
                need_full(f"migration {name}: quyền theo cả schema")
            touched.add(qualified(obj, name))
            kind = next(k for k in ("create", "alter", "index", "trigger", "policy", "grant", "insert", "update") if m.group(k))
            # Tạo đối tượng MỚI (bảng, view, hàm, kiểu) không đổi đối tượng nó nhắc tới; còn lại (ALTER TABLE … REFERENCES,
            # bảng PARTITION OF / INHERITS, trigger, policy, index, quyền, ghi dữ liệu) có thể đổi hành vi của mọi đối
            # tượng trong câu (vd khóa ngoại mới chặn xóa ở bảng cha) → tính tất cả.
            if kind != "create" or re.search(r"(?i)\b(PARTITION\s+OF|INHERITS)\b", st):
                touched |= {x.lower() for x in obj_ref.findall(st)}
    # Nối qua view, hàm, trigger, policy (gom từ MỌI migration, mọi bản định nghĩa — rộng hơn cần thiết, không sót):
    # view đọc đối tượng bị chạm → view bị chạm (người dùng view phải test lại); hàm dùng đối tượng bị chạm → hàm và mọi
    # đối tượng nó nhắc tới bị chạm; trigger, policy dùng hàm hay bảng bị chạm → bảng của nó bị chạm (người ghi bảng đó
    # chạy lại trigger, policy). Chiều ngược (bảng của trigger bị chạm) không cần lan sang hàm: người ghi bảng đã được
    # test, mà lan thì một hàm dùng chung (vd đặt updated_at) kéo theo mọi bảng.
    defs = []
    for t in texts.values():
        for st in statements(strip_sql(t)):
            mv = re.match(r"(?is)\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:MATERIALIZED\s+)?VIEW\s+(?:IF\s+NOT\s+EXISTS\s+)?([\w.\"]+)", st)
            refs = {x.lower() for x in obj_ref.findall(st)}
            mt = re.match(r"(?is)\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:"
                          r"(?:CONSTRAINT\s+)?TRIGGER\s+\w+\s+(?:BEFORE|AFTER|INSTEAD\s+OF)\s+[\w\s,]+?\s+ON"
                          r"|POLICY\s+\w+\s+ON|RULE\s+\w+\s+AS\s+ON\s+\w+\s+TO)\s+([\w.\"]+)", st)
            if mv:
                defs.append(({mv.group(1).replace('"', "").lower()}, refs))
            elif mt:
                tbl = mt.group(1).replace('"', "").lower()
                defs.append(({tbl}, refs - {tbl}))
            elif re.match(r"(?is)\s*CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\b", st):
                defs.append((refs, refs))
    grew = True
    while grew:
        grew = False
        for outs, refs in defs:
            if refs & touched and not outs <= touched:
                touched |= outs
                grew = True
    return touched


def sqlc_dirs():
    """queries/<x> → thư mục mã sqlc sinh ra (sqlc.yaml)."""
    out, q = {}, None
    for line in open(os.path.join(root, "sqlc.yaml"), encoding="utf-8"):
        m = re.match(r"\s*queries:\s*(\S+)", line)
        if m:
            q = m.group(1).rstrip("/")
        m = re.match(r"\s*out:\s*(\S+)", line)
        if m and q:
            out[q] = m.group(1).rstrip("/")
            q = None
    return out


mig_code_dirs, mig_test_dirs = set(), set()
migs = [p for p in changed if MIG_RE.match(p)]
if base is not None and migs:
    touched = migration_scope(migs)
    # Tên đầy đủ schema.tên, đúng cả từ (catalog.products không khớp catalog.products_v).
    use = re.compile(r"(?i)(?<![\w.])(" + "|".join(re.escape(o) for o in sorted(touched)) + r")(?!\w)")
    qmap = sqlc_dirs()
    files = ""
    if touched:     # chỉ COMMENT ON, SET…: không đối tượng nào (regex rỗng sẽ khớp mọi tệp)
        files = subprocess.run(["git", "ls-files", "-z", "--", "queries", "*.go"], capture_output=True, text=True, cwd=root).stdout
    for f in filter(None, (nfc(x) for x in files.split("\0"))):
        if f.startswith((".worktrees/", "spikes/", "console/")) or "/internal/store/" in f or f.startswith("internal/httpapi/gen/"):
            continue
        try:
            body = open(os.path.join(root, f), encoding="utf-8").read()
        except OSError:
            continue
        if not use.search(body):
            continue
        if f.startswith("queries/"):
            d = qmap.get(os.path.dirname(f))
            if d is None:
                need_full(f"{f}: thư mục câu SQL không có trong sqlc.yaml")
            mig_code_dirs.add(d)
        elif f.endswith("_test.go"):
            mig_test_dirs.add(os.path.dirname(f))
        else:
            mig_code_dirs.add(os.path.dirname(f))
    # Luôn chạy test của chính các gói (không lan: mọi gói nhập testkit): bộ chạy migration, mẫu database của test, và
    # tenancy — TestCheckSchemaRulesCleanSchema soát MỌI bảng, view sau migrate (RLS, unique theo tenant_id) mà không
    # nhắc tên bảng nào, nên bảng mới quên enable_tenant_rls chỉ lộ ở đó.
    mig_test_dirs |= {"migrations", "internal/platform/db", "internal/testkit", "internal/platform/tenancy"}


# ---- Tệp dữ liệu (Task 478, 24/11) ----
# Lỗ đã gặp: Task 192 đổi openapi/openapi.yaml; test của tools/routegen đọc tệp đó bằng đường dẫn, không qua import Go,
# nên không gói nào được chọn và main đỏ. Tệp dữ liệu là mọi tệp không phải .go (cùng mọi tệp trong testdata/), trừ
# go.mod/go.sum, migrations/ (luật riêng ở trên), module riêng (spikes/) và bản sao worktree.
def is_data(p):
    if os.path.basename(p) in ("go.mod", "go.sum") or p.startswith(("migrations/", "spikes/", ".worktrees/")):
        return False
    return "testdata" in p.split("/") or not p.endswith(".go")


data_files = [p for p in changed if is_data(p)]
go_dirs = {os.path.dirname(p) or "." for p in changed if (p.endswith(".go") or p.endswith("go.mod") or p.endswith("go.sum")) and not is_data(p)}
if not go_dirs and not mig_code_dirs and not mig_test_dirs and not data_files:
    sys.exit(0)
high_risk = os.environ.get("HIGH_RISK_DIRS", "").split()

out = subprocess.run(["go", "list", "-json", "./..."], capture_output=True, text=True, cwd=root)
if out.returncode != 0:
    sys.stderr.write(out.stderr)
    sys.exit(3)
dec = json.JSONDecoder(); s = out.stdout; i = 0; pkgs = []
while i < len(s):
    while i < len(s) and s[i].isspace(): i += 1
    if i >= len(s): break
    obj, i = dec.raw_decode(s, i); pkgs.append(obj)

by_dir = {os.path.relpath(nfc(p["Dir"]), root): p["ImportPath"] for p in pkgs}
# Lan lên theo phụ thuộc thật của Go (19/11): gói X nhập Y trong mã chạy (Imports) thì mọi gói dùng X cũng dùng Y -> lan tiếp.
# Gói X chỉ nhập Y trong test (TestImports/XTestImports) thì chỉ test của X phải chạy lại; gói dùng X KHÔNG biên dịch test
# của X nên không lan tiếp. Trước đây gộp chung nên đổi một module kéo theo gần cả bộ (test của realtime nhập httpapi,
# testkit nhập realtime -> mọi gói dùng testkit).
importers, test_importers = {}, {}
for p in pkgs:
    for imp in p.get("Imports", []):
        importers.setdefault(imp, set()).add(p["ImportPath"])
    for imp in p.get("TestImports", []) + p.get("XTestImports", []):
        test_importers.setdefault(imp, set()).add(p["ImportPath"])


def spread(start):
    reached, stack = set(start), list(start)
    while stack:
        cur = stack.pop()
        for up in importers.get(cur, ()):
            if up not in reached:
                reached.add(up); stack.append(up)
    return reached


targets = set()
if any(d in (".",) for d in go_dirs) and any(p in ("go.mod", "go.sum") for p in changed):
    targets = {p["ImportPath"] for p in pkgs}          # đổi phụ thuộc: test hết
# Thư mục chỉ đổi tệp _test.go: chỉ chạy lại test của chính gói đó, không lan (20/11, chủ shop "chỉ test những case ảnh
# hưởng"): tệp _test.go không được biên dịch vào gói nào nhập gói này, nên mã và test của các gói kia không đổi.
code_dirs = {os.path.dirname(p) or "." for p in changed if (p.endswith(".go") and not p.endswith("_test.go")) or p.endswith("go.mod") or p.endswith("go.sum")}
test_only = set()
for d in go_dirs:
    if d not in by_dir: continue
    if d in code_dirs: targets.add(by_dir[d])
    else: test_only.add(by_dir[d])
# Gói bị sửa trực tiếp (chạy -race): mã viết tay hay test của nó đổi. Mã sinh (sqlc store, ogen gen, bảng route) đổi
# theo nguồn ở nơi khác (vd thêm cột → models.go của mọi module): vẫn lan lên gói dùng nó, nhưng không tính là sửa trực tiếp.
GEN_RE = re.compile(r"(^|/)internal/store/|^internal/httpapi/gen/|^internal/httpapi/routes_gen\.go$")
hand_dirs = {os.path.dirname(p) or "." for p in changed if p.endswith(".go") and not GEN_RE.search(p)}
direct = {by_dir[d] for d in hand_dirs if d in by_dir}
if any(p in ("go.mod", "go.sum") for p in changed):
    direct = set(targets)                            # đổi phụ thuộc: mọi gói chạy -race

# Tệp dữ liệu (Task 478):
# (a) Tệp trong thư mục một gói (go:embed, tệp gói đọc lúc chạy; thư mục con không phải gói thì lấy gói gần nhất phía
#     trên, không tính gốc repo) → coi như mã của gói đổi, lan lên gói nhập nó. Tệp trong <gói>/testdata/ → chỉ test
#     của gói đó, trừ khi mã chạy nhúng nó (EmbedFiles của go list, vd carriertest nhúng testdata: tính cả thư mục
#     nhúng để bắt tệp mới, tệp xóa) → như mã. Tài liệu .md ngoài thư mục gói: không theo (a), chỉ theo (b).
# (b) Tên tệp (basename) nằm trong một chuỗi Go của tệp _test.go → test của gói chứa tệp test đó (không lan): test đọc
#     tệp theo đường dẫn, vd filepath.Join("..", "..", "openapi", "openapi.yaml") của tools/routegen.
# (c) Tệp dữ liệu không gây race: gói thêm theo (a), (b) xếp "plain" (không vào direct), trừ khi mã viết tay của gói đổi.
# Không bắt: test dựng tên tệp lúc chạy (nối chuỗi, glob theo đuôi) — lưới an toàn vẫn là toàn bộ test trên main.
def owner_dir(f):
    d = os.path.dirname(f)
    while d and d not in by_dir:
        d = os.path.dirname(d)
    return d or None


data_code, data_test, data_names = set(), set(), {}     # data_names: tên tệp → các đường dẫn (tách theo "/")
if data_files:
    embed_code = {}     # tệp, thư mục con mà mã chạy của gói nhúng → các gói
    for p in pkgs:
        pdir = os.path.relpath(nfc(p["Dir"]), root)
        for e in p.get("EmbedFiles", []):
            f = os.path.normpath(os.path.join(pdir, nfc(e)))
            while len(f) > len(pdir) and f.startswith(pdir + "/"):
                embed_code.setdefault(f, set()).add(p["ImportPath"])
                f = os.path.dirname(f)
    for f in data_files:
        hit, d = set(), f          # tệp hay một thư mục chứa nó (kể cả thư mục con mới tạo) được mã chạy nhúng
        while d and not hit:
            hit = embed_code.get(d, set()); d = os.path.dirname(d)
        parts = f.split("/")
        if hit:
            data_code |= hit
        elif "testdata" in parts:
            td = owner_dir("/".join(parts[:parts.index("testdata") + 1]))   # gói gần nhất chứa thư mục testdata
            if td:
                data_test.add(by_dir[td])
        elif not f.endswith(".md") or owner_dir(f):   # tài liệu .md ngoài thư mục gói: không như mã gói nào
            od = owner_dir(f)
            if od:
                data_code.add(by_dir[od])
        # (b) áp cho mọi tệp dữ liệu, cả tài liệu (test đọc docs/quyen.md, docs/ca-chay-tay.md theo đường dẫn).
        data_names.setdefault(os.path.basename(f), []).append(parts)
    if data_names:
        # Tên tệp kèm phần đường dẫn đứng trước nó trong chuỗi ("docker/compose.dev.yml", "testdata/ts/index.ts"): phần
        # đó (bỏ ".", "..") phải là đuôi đường dẫn của tệp đổi, không thì chỉ trùng tên (testdata/ts/index.ts của
        # tools/catalog không phải console/src/api/index.ts).
        name_re = re.compile(r"(?<![\w./-])((?:[\w.-]*/)*)(" + "|".join(re.escape(n) for n in sorted(data_names)) + r")(?![\w.-])")
        # Quét token từ trái sang: chú thích, rune ('"') bị nuốt nguyên khối, chỉ giữ chuỗi "…" và `…`.
        tok_re = re.compile(r'"(?:[^"\\\n]|\\.)*"|`[^`]*`|//[^\n]*|/\*.*?\*/|\'(?:[^\'\\\n]|\\.)*\'', re.S)

        def refers(s):
            for m in name_re.finditer(s):
                comps = [c for c in m.group(1).split("/") if c not in ("", ".", "..")] + [m.group(2)]
                if any(p[-len(comps):] == comps for p in data_names[m.group(2)]):
                    return True
            return False
        tests_ = subprocess.run(["git", "ls-files", "-z", "--", "*_test.go"], capture_output=True, text=True, cwd=root).stdout
        for f in filter(None, (nfc(x) for x in tests_.split("\0"))):
            if f.startswith((".worktrees/", "spikes/", "console/")) or os.path.dirname(f) not in by_dir:
                continue
            try:
                body = open(os.path.join(root, f), encoding="utf-8").read()
            except OSError:
                continue
            if any(s[0] in "\"`" and refers(s) for s in tok_re.findall(body)):
                data_test.add(by_dir[os.path.dirname(f)])
test_only |= data_test
reached = spread(targets)                    # gói có mã chạy bị ảnh hưởng (lan tiếp được)
reached |= spread(data_code)
reached |= spread({by_dir[d] for d in mig_code_dirs if d in by_dir})
targets = set(reached) | test_only | {by_dir[d] for d in mig_test_dirs if d in by_dir}
for cur in reached:             # test của gói nhập gói bị ảnh hưởng: chạy lại, không lan tiếp
    targets |= test_importers.get(cur, set())
for hr in high_risk:
    hits = [p for p in changed if (p == hr or p.startswith(hr.rstrip("/") + "/")) and not MIG_RE.match(p) and not GEN_RE.search(p)]
    if hits:
        # Chỉ tệp dữ liệu chạm vùng (Task 478): vẫn chạy cả bộ của vùng nhưng test thường, không -race.
        hand = any(not is_data(p) for p in hits)
        hb = hr if os.path.isdir(os.path.join(root, hr)) else os.path.dirname(hr)
        for dd, ip in by_dir.items():
            if dd == hb or dd.startswith(hb + "/"):
                targets.add(ip)
                if hand:
                    direct.add(ip)
# Chặn hỏng âm thầm: thư mục có tệp .go đổi (còn tồn tại) mà không khớp gói nào -> thoát 4, ci-task chạy toàn bộ test.
unmatched = [d for d in go_dirs if d != "." and d not in by_dir and os.path.isdir(os.path.join(root, d))
             and any(f.endswith(".go") for f in os.listdir(os.path.join(root, d)))
             and not d.startswith(("console", "spikes")) and "testdata" not in d.split("/")]
if unmatched:
    sys.stderr.write("affected.py: không khớp gói cho " + ", ".join(sorted(unmatched)) + "\n")
    sys.exit(4)
if split:
    print("\n".join(("race " if p in direct else "plain ") + p for p in sorted(targets)))
else:
    print("\n".join(sorted(targets)))
