#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Công cụ đồng bộ tự động dữ liệu từ CMS Demego (https://cms.demego.vn/api/posts)
lên Knowledge Base (Dataset) RAGFlow Production (https://faq.demego.vn).
"""

import os
import re
import sys
import time
import html
import requests
from concurrent.futures import ThreadPoolExecutor, as_completed

# Thiết lập UTF-8 stdout cho Windows console
if sys.platform == "win32":
    import codecs
    sys.stdout = codecs.getwriter("utf-8")(sys.stdout.detach())
    sys.stderr = codecs.getwriter("utf-8")(sys.stderr.detach())

# Cấu hình mặc định
CMS_API_BASE = os.getenv("CMS_API_BASE", "https://cms.demego.vn/api")
RAGFLOW_API_BASE = os.getenv("RAGFLOW_API_BASE", "https://faq.demego.vn/api/v1")
RAGFLOW_API_KEY = os.getenv("RAGFLOW_API_KEY", "ragflow-G4BMCYj526XtYPRWNLe9YPwZ_MH2SO233QJOT_-U-Jw")
TARGET_DATASET_NAME = os.getenv("TARGET_DATASET_NAME", "CMS-DEMEGO")

HEADERS = {
    "Authorization": f"Bearer {RAGFLOW_API_KEY}"
}

def clean_filename(title: str, max_len: int = 100) -> str:
    """Tạo tên file an toàn cho hệ thống."""
    # Thay thế ký tự đặc biệt
    clean = re.sub(r'[\\/*?:"<>|]', '_', title)
    clean = re.sub(r'\s+', '_', clean).strip('_')
    if not clean:
        clean = "bai_viet"
    return clean[:max_len]

def html_to_clean_markdown(html_text: str) -> str:
    """Chuyển đổi nội dung HTML từ CMS sang định dạng Markdown chuẩn cho RAGFlow."""
    if not html_text:
        return ""
    
    text = html.unescape(html_text)
    # Loại bỏ thẻ style và script
    text = re.sub(r'<script.*?</script>', '', text, flags=re.DOTALL | re.IGNORECASE)
    text = re.sub(r'<style.*?</style>', '', text, flags=re.DOTALL | re.IGNORECASE)
    
    # Chuẩn hóa tiêu đề
    text = re.sub(r'<h1[^>]*>(.*?)</h1>', r'\n# \1\n', text, flags=re.IGNORECASE)
    text = re.sub(r'<h2[^>]*>(.*?)</h2>', r'\n## \1\n', text, flags=re.IGNORECASE)
    text = re.sub(r'<h3[^>]*>(.*?)</h3>', r'\n### \1\n', text, flags=re.IGNORECASE)
    text = re.sub(r'<h4[^>]*>(.*?)</h4>', r'\n#### \1\n', text, flags=re.IGNORECASE)
    
    # Đoạn văn và ngắt dòng
    text = re.sub(r'<p[^>]*>(.*?)</p>', r'\n\1\n', text, flags=re.IGNORECASE)
    text = re.sub(r'<li[^>]*>(.*?)</li>', r'\n* \1', text, flags=re.IGNORECASE)
    text = re.sub(r'<br\s*/?>', '\n', text, flags=re.IGNORECASE)
    
    # Xóa toàn bộ thẻ HTML còn lại
    text = re.sub(r'<[^>]+>', '', text)
    
    # Rút gọn khoảng trống dư thừa
    text = re.sub(r'\n{3,}', '\n\n', text)
    return text.strip()

def get_dataset_id(dataset_name: str) -> str:
    """Lấy Dataset ID dựa trên tên Dataset trong RAGFlow."""
    url = f"{RAGFLOW_API_BASE}/datasets?page=1&page_size=50"
    res = requests.get(url, headers=HEADERS, timeout=15)
    res.raise_for_status()
    datasets = res.json().get("data", [])
    for d in datasets:
        if d.get("name", "").strip().upper() == dataset_name.strip().upper():
            return d.get("id")
    raise ValueError(f"Không tìm thấy Dataset với tên '{dataset_name}' trên RAGFlow!")

def get_all_cms_posts():
    """Lấy toàn bộ danh sách bài viết từ CMS Demego API."""
    print(">>> Đang lấy danh sách bài viết từ CMS Demego...")
    posts = []
    page = 1
    limit = 50
    total = None

    while True:
        url = f"{CMS_API_BASE}/posts?page={page}&limit={limit}"
        res = requests.get(url, timeout=20)
        res.raise_for_status()
        data = res.json()
        
        batch = data.get("posts", [])
        if not batch:
            break
            
        posts.extend(batch)
        total = data.get("total", len(posts))
        print(f"  [+] Đã tải danh sách trang {page} ({len(posts)}/{total} bài viết)")
        
        if len(posts) >= total or len(batch) < limit:
            break
        page += 1

    print(f"==> Tổng cộng có {len(posts)} bài viết từ CMS Demego.")
    return posts

def fetch_post_detail(post_item):
    """Lấy chi tiết nội dung đầy đủ của 1 bài viết theo _id."""
    post_id = post_item.get("_id")
    try:
        url = f"{CMS_API_BASE}/posts/{post_id}"
        res = requests.get(url, timeout=15)
        if res.status_code == 200:
            return res.json()
    except Exception as e:
        print(f"  [!] Lỗi khi lấy chi tiết bài viết {post_id}: {e}")
    return post_item

def prepare_markdown_content(detail):
    """Đóng gói tiêu đề, metadata và nội dung bài viết thành tài liệu Markdown."""
    title_obj = detail.get("title", {})
    title = title_obj.get("vi") or title_obj.get("en") or "Không tiêu đề"
    
    slug_obj = detail.get("slug", {})
    slug = slug_obj.get("vi") or slug_obj.get("en") or ""
    
    excerpt_obj = detail.get("excerpt", {})
    excerpt = excerpt_obj.get("vi") or excerpt_obj.get("en") or ""
    
    content_obj = detail.get("content", {})
    raw_content = ""
    if isinstance(content_obj, dict):
        raw_content = content_obj.get("vi") or content_obj.get("en") or ""
    elif isinstance(content_obj, str):
        raw_content = content_obj

    body_markdown = html_to_clean_markdown(raw_content)
    
    post_url = f"https://cms.demego.vn/posts/{slug}" if slug else "https://cms.demego.vn"
    
    doc_text = f"""# {title}

**Nguồn:** {post_url}  
**Mã bài viết:** `{detail.get('_id')}`  
**Ngày cập nhật:** {detail.get('updatedAt', '')}  

---

### Tóm tắt
{html_to_clean_markdown(excerpt) if excerpt else 'Không có tóm tắt'}

---

### Nội dung chi tiết
{body_markdown if body_markdown else 'Bài viết không có nội dung văn bản chi tiết.'}
"""
    filename = f"{clean_filename(title)}_{detail.get('_id')[-6:]}.md"
    meta_fields = {
        "source": "cms.demego.vn",
        "post_id": str(detail.get("_id") or ""),
        "slug": slug,
        "url": post_url,
        "menu_id": str(detail.get("menuId") or ""),
        "status": str(detail.get("status") or "published"),
        "updated_at": str(detail.get("updatedAt") or "")
    }
    return filename, doc_text, meta_fields

def upload_and_parse_document(dataset_id: str, filename: str, doc_content: str, meta_fields: dict = None):
    """Đẩy tài liệu lên RAGFlow, gán Metadata và kích hoạt xử lý Parse/Chunking."""
    upload_url = f"{RAGFLOW_API_BASE}/datasets/{dataset_id}/documents"
    
    files = {
        'file': (filename, doc_content.encode('utf-8'), 'text/markdown')
    }
    
    res = requests.post(upload_url, headers=HEADERS, files=files, timeout=30)
    if res.status_code != 200:
        return False, f"Upload thất bại: {res.text}"
    
    data = res.json().get("data", [])
    doc_id = data[0].get("id") if isinstance(data, list) and data else (data.get("id") if isinstance(data, dict) else None)
    
    if not doc_id:
        return False, "Không lấy được document ID sau khi upload"

    if meta_fields:
        try:
            requests.patch(
                f"{RAGFLOW_API_BASE}/datasets/{dataset_id}/documents/{doc_id}",
                headers=HEADERS,
                json={"meta_fields": meta_fields},
                timeout=15
            )
        except Exception:
            pass
        
    # Kích hoạt Parse Chunking ngay
    chunks_url = f"{RAGFLOW_API_BASE}/datasets/{dataset_id}/chunks"
    try:
        requests.post(chunks_url, headers=HEADERS, json={'document_ids': [doc_id], 'run': '1'}, timeout=15)
    except Exception:
        pass # RAGFlow background ingestor sẽ tự động xếp hàng parse
        
    return True, doc_id

def main():
    print("==================================================================")
    print("🚀 BẮT ĐẦU ĐỒNG BỘ NỘI DUNG CMS DEMEGO LÊN RAGFLOW PRODUCTION")
    print(f"Target URL: {RAGFLOW_API_BASE}")
    print(f"Target Dataset: {TARGET_DATASET_NAME}")
    print("==================================================================")
    
    # 1. Lấy Dataset ID
    try:
        dataset_id = get_dataset_id(TARGET_DATASET_NAME)
        print(f"✔ Đã xác định Dataset ID: {dataset_id}")
    except Exception as e:
        print(f"❌ Lỗi: {e}")
        sys.exit(1)

    # 2. Lấy toàn bộ bài viết từ CMS
    posts = get_all_cms_posts()
    if not posts:
        print("❌ Không có bài viết nào từ CMS.")
        return

    # 3. Lấy chi tiết song song (đa luồng)
    print(f"\n>>> Đang tải nội dung chi tiết cho {len(posts)} bài viết...")
    detailed_posts = []
    with ThreadPoolExecutor(max_workers=8) as executor:
        futures = {executor.submit(fetch_post_detail, p): p for p in posts}
        count = 0
        for f in as_completed(futures):
            res = f.result()
            if res:
                detailed_posts.append(res)
            count += 1
            if count % 30 == 0 or count == len(posts):
                print(f"  [+] Đã tải nội dung chi tiết: {count}/{len(posts)}")

    # 4. Đẩy tài liệu lên RAGFlow
    print(f"\n>>> Đang đẩy và kích hoạt xử lý {len(detailed_posts)} tài liệu lên RAGFlow...")
    success_count = 0
    fail_count = 0

    def process_upload(post):
        filename, content, meta_fields = prepare_markdown_content(post)
        ok, msg = upload_and_parse_document(dataset_id, filename, content, meta_fields)
        return ok, filename, msg

    with ThreadPoolExecutor(max_workers=5) as executor:
        futures = {executor.submit(process_upload, p): p for p in detailed_posts}
        total_upload = len(detailed_posts)
        done = 0
        for f in as_completed(futures):
            ok, fname, msg = f.result()
            done += 1
            if ok:
                success_count += 1
            else:
                fail_count += 1
                print(f"  [-] Lỗi upload {fname}: {msg}")
                
            if done % 20 == 0 or done == total_upload:
                print(f"  [==>] Tiến độ đẩy tài liệu: {done}/{total_upload} (Thành công: {success_count}, Thất bại: {fail_count})")

    print("\n==================================================================")
    print(f"🎉 HOÀN TẤT ĐỒNG BỘ:")
    print(f" - Tổng bài viết CMS: {len(detailed_posts)}")
    print(f" - Thành công đẩy lên RAGFlow: {success_count}")
    print(f" - Thất bại: {fail_count}")
    print(" - Các tài liệu đang được Ingestor parse & vectorize tự động trên server.")
    print("==================================================================")

if __name__ == "__main__":
    main()
