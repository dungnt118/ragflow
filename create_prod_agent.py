#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Công cụ khởi tạo & xuất bản Agent QLTN thông minh trên RAGFlow Production (https://faq.demego.vn).
Kết hợp:
- Mô hình LLM: deepseek-v4-pro@RAG-DEEPSEEK@DeepSeek
- Kho tri thức (Retrieval): CMS-DEMEGO (243 tài liệu) + BA-DEMEGO (146 tài liệu)
- Công cụ trực tuyến (MCP): QLTN-Sandbox-Mulberrylane (23 tools tra cứu nghiệp vụ, schema, dữ liệu, báo cáo, mẫu in, luồng tự động)
"""

import os
import sys
import codecs
import requests

if sys.platform == "win32":
    sys.stdout = codecs.getwriter("utf-8")(sys.stdout.detach())
    sys.stderr = codecs.getwriter("utf-8")(sys.stderr.detach())

RAGFLOW_API_BASE = os.getenv("RAGFLOW_API_BASE", "https://faq.demego.vn/api/v1")
RAGFLOW_API_KEY = os.getenv("RAGFLOW_API_KEY", "ragflow-G4BMCYj526XtYPRWNLe9YPwZ_MH2SO233QJOT_-U-Jw")
HEADERS = {"Authorization": f"Bearer {RAGFLOW_API_KEY}"}

MCP_ID = "1a09772a54724f1793c40445d335e2b0"
DATASET_CMS_ID = "957ce2a147544d1786ea9db74ec35951"
DATASET_BA_ID = "8943e2e00b0e40458228fd770095f025"

def main():
    print("==================================================================")
    print("🚀 ĐANG KHỞI TẠO AGENT QLTN TRÊN RAGFLOW PRODUCTION")
    print("==================================================================")

    # 1. Lấy danh sách MCP tools từ server đã khai báo
    r_mcp = requests.get(f"{RAGFLOW_API_BASE}/mcp/servers/{MCP_ID}", headers=HEADERS, timeout=15)
    r_mcp.raise_for_status()
    all_tools = r_mcp.json().get("data", {}).get("variables", {}).get("tools", {})

    selected_tool_names = [
        "search_knowledge_articles",
        "search_knowledge_categorys",
        "meta-mcp-search_knowledge",
        "meta-mcp-get_knowledge",
        "schema-list",
        "schema-search",
        "schema-get",
        "schema-get_relationships",
        "schema-grouped_by_tags",
        "data-search",
        "data-get",
        "data-count",
        "issue_search",
        "issue_get",
        "dataflow-list",
        "dataflow-search",
        "dataflow-get",
        "datasource-search",
        "datasource-get",
        "print_template_search",
        "print_template_get",
        "apijob-cron-list",
        "apijob-trigger-list",
    ]

    selected_mcp_tools = {k: all_tools[k] for k in selected_tool_names if k in all_tools}
    print(f"✔ Đã trích xuất {len(selected_mcp_tools)} công cụ MCP trọng tâm từ QLTN-Sandbox-Mulberrylane.")

    sys_prompt = """# VAI TRÒ & PHONG THÁI
Bạn là Chuyên gia Cố vấn Vận hành, Nghiệp vụ & Kỹ thuật cấp cao của Hệ thống Quản lý Tòa nhà / Khu đô thị (Demego / DemePro / QLTN).
- Phong thái: Tự nhiên, thân thiện, lịch sự, chuẩn xác và sâu sắc như một chuyên gia am hiểu tường tận hệ thống đang trực tiếp hướng dẫn đồng nghiệp/khách hàng.
- Xưng hô: "Em" (hoặc "mình") và gọi người dùng là "Anh/Chị" (hoặc "bạn").
- Khi người dùng chỉ chào hỏi xã giao (ví dụ: "xin chào", "hi"), hãy chào lại ngắn gọn, niềm nở trong 1-2 câu: "Dạ chào anh/chị! Em có thể hỗ trợ gì cho anh/chị về nghiệp vụ, hướng dẫn thao tác hay tra cứu dữ liệu trên hệ thống QLTN hôm nay ạ?" (TUYỆT ĐỐI KHÔNG liệt kê danh sách tính năng dài dòng khi chưa được hỏi).

# CẤM KỴ TUYỆT ĐỐI
- KHÔNG BAO GIỜ nói những câu máy móc lộ cơ chế nội bộ như: "Tôi là trợ lý AI...", "Để tôi gọi công cụ Retrieval...", "Dựa trên dataset CMS-DEMEGO...", "Theo kết quả từ MCP...".
- Hãy trình bày kiến thức một cách tự tin, mạch lạc, có đầu có cuối như tri thức chuyên môn của chính bạn.
- Không tự bịa đặt thông tin, quy trình hay dữ liệu không có thật. Khi đã tra cứu cả tài liệu lẫn hệ thống mà không có thông tin, hãy trả lời khéo léo: "Dạ phần này hiện trên hệ thống chưa có quy trình/dữ liệu ghi nhận cụ thể, anh/chị có muốn em kiểm tra theo hướng khác không ạ?".

# CHIẾN LƯỢC SUY LUẬN & KHAI THÁC TRI THỨC ĐA TẦNG (MULTI-STEP REASONING)
Bạn sở hữu 2 nguồn sức mạnh:
1. **Kho Tri Thức Tập Trung (Retrieval Tool - kết nối cả CMS-DEMEGO và BA-DEMEGO)**:
   - Chứa 243 bài viết hướng dẫn sử dụng chi tiết từng màn hình (Web BQL, App Cư dân) và 146 tài liệu đặc tả nghiệp vụ BA, kịch bản xử lý tình huống thực tế.
   - **Nguyên tắc:** Với mọi câu hỏi về cách sử dụng, quy trình nghiệp vụ (cư dân, căn hộ, thẻ xe, hóa đơn, thu chi, đăng ký thi công, tiện ích, phản ánh, thông báo...), LUÔN gọi công cụ `Retrieval` đầu tiên (có thể thử với từ khóa chính xác và từ khóa mở rộng).
2. **Hệ Thống Công Cụ Trực Tuyến (MCP QLTN Tools)**:
   - Dùng để tra cứu bài viết hướng dẫn trực tiếp (`search_knowledge_articles`, `search_knowledge_categorys`), khám phá cấu trúc dữ liệu (`schema-search`, `schema-get`, `schema-get_relationships`), truy vấn dữ liệu thực tế (`data-search`, `data-get`, `data-count`), tra cứu quy trình tự động (`dataflow-search`, `apijob-cron-list`, `apijob-trigger-list`), báo cáo (`datasource-search`), mẫu in (`print_template_search`), và lịch sử xử lý lỗi (`issue_search`).
   - **Nguyên tắc:**
     + Khi người dùng hỏi về dữ liệu thực tế, cấu trúc bảng/schema, luồng tự động hoặc báo cáo -> Chủ động gọi MCP Tools tương ứng.
     + Khi tra cứu `Retrieval` chưa đủ chi tiết -> Chủ động gọi bổ sung `search_knowledge_articles` hoặc `schema-search` trên MCP để có câu trả lời toàn diện nhất.

# CẤU HÌNH ĐƯỜNG DẪN TRUY CẬP TRỰC TIẾP
- Tên miền cơ sở (Base URL) của hệ thống Web BQL: https://mulberrylane.sandbox.demego.vn
- Mỗi khi hướng dẫn thao tác trên một màn hình hoặc chức năng cụ thể, nếu trong tài liệu có đường dẫn hoặc nguồn bài viết, hãy đính kèm link trực tiếp để Anh/Chị tiện bấm vào mở ngay.

# QUY TẮC TRÌNH BÀY RÕ RÀNG, DỄ ĐỌC
- Sử dụng các biểu tượng Emoji trực quan ở đầu mỗi mục (ví dụ: 👉, 📌, 🔹, 📍, 💡, ✅) để phân tách các bước rõ ràng.
- Trình bày theo từng bước (Bước 1, Bước 2...) đối với câu hỏi hướng dẫn thao tác.
- Giải thích rõ ràng cả "Cách làm" (Thao tác ở đâu, bấm nút nào) và "Lưu ý nghiệp vụ" (Điều kiện tiên quyết, trạng thái phiếu, phân quyền liên quan)."""

    prologue_text = """Chào Anh/Chị! 👋

Em là Trợ lý Chuyên gia Hỗ trợ Vận hành & Nghiệp vụ Hệ thống Quản lý Tòa nhà Demego (QLTN).

Em có thể hỗ trợ Anh/Chị:
🔹 Hướng dẫn chi tiết quy trình nghiệp vụ: Cư dân, Căn hộ, Thẻ xe, Hóa đơn, Thu chi kế toán, Đăng ký thi công, Phản ánh...
🔹 Hướng dẫn thao tác từng bước trên Web BQL và App Cư dân (CMS & BA Knowledge Base)
🔹 Tra cứu trực tiếp Cấu trúc dữ liệu (Schema), Dữ liệu thực tế, Báo cáo, Mẫu in & Luồng tự động qua MCP

Anh/Chị đang cần hỗ trợ vấn đề gì, cứ nhắn cho em nhé! 😊"""

    retrieval_tool = {
        "component_name": "Retrieval",
        "id": "Retrieval:WarmWeeksRead",
        "name": "Retrieval",
        "params": {
            "cross_languages": [],
            "dataset_ids": [DATASET_CMS_ID, DATASET_BA_ID],
            "description": "Kho tri thức chính thức bao gồm toàn bộ 243 bài viết hướng dẫn sử dụng CMS Demego (Web BQL & App Cư dân) và 146 tài liệu đặc tả nghiệp vụ BA, quy trình vận hành tòa nhà. Luôn gọi công cụ này để tra cứu hướng dẫn thao tác và quy trình nghiệp vụ.",
            "empty_response": "",
            "keywords_similarity_weight": 0.7,
            "outputs": {
                "formalized_content": {
                    "type": "string",
                    "value": ""
                }
            },
            "rerank_id": "",
            "retrieval_from": "dataset",
            "similarity_threshold": 0.2,
            "top_k": 1024,
            "top_n": 10,
            "use_kg": False
        }
    }

    agent_params = {
        "delay_after_error": 1,
        "description": "Trợ lý AI thông minh kết hợp Knowledge Base (CMS + BA) và hệ thống công cụ MCP QLTN",
        "exception_comment": "",
        "exception_default_value": "",
        "exception_goto": [],
        "exception_method": None,
        "frequencyPenaltyEnabled": True,
        "frequency_penalty": 0.1,
        "llm_id": "deepseek-v4-pro@RAG-DEEPSEEK@DeepSeek",
        "maxTokensEnabled": False,
        "max_retries": 3,
        "max_rounds": 8,
        "max_tokens": 8192,
        "mcp": [
            {
                "mcp_id": MCP_ID,
                "tools": selected_mcp_tools
            }
        ],
        "message_history_window_size": 12,
        "outputs": {
            "content": {
                "type": "string",
                "value": ""
            }
        },
        "parameter": "Precise",
        "presencePenaltyEnabled": True,
        "presence_penalty": 0.1,
        "prompts": [
            {
                "content": "The user query is {sys.query}",
                "role": "user"
            }
        ],
        "sys_prompt": sys_prompt,
        "temperature": 0.3,
        "temperatureEnabled": True,
        "tool_timeout": 20,
        "tools": [retrieval_tool],
        "topPEnabled": True,
        "top_p": 0.85,
        "user_prompt": "",
        "visual_files_var": ""
    }

    dsl = {
        "components": {
            "begin": {
                "downstream": ["Agent:StalePandasDream"],
                "obj": {
                    "component_name": "Begin",
                    "params": {
                        "enablePrologue": True,
                        "inputs": {},
                        "mode": "conversational",
                        "outputs": {},
                        "prologue": prologue_text
                    }
                },
                "upstream": []
            },
            "Agent:StalePandasDream": {
                "downstream": ["Message:BrownPugsStick"],
                "obj": {
                    "component_name": "Agent",
                    "params": agent_params
                },
                "upstream": ["begin"]
            },
            "Message:BrownPugsStick": {
                "downstream": [],
                "obj": {
                    "component_name": "Message",
                    "params": {
                        "content": ["{Agent:StalePandasDream@content}"],
                        "output_format": "markdown"
                    }
                },
                "upstream": ["Agent:StalePandasDream"]
            }
        },
        "globals": {
            "env.base_url": "https://mulberrylane.sandbox.demego.vn",
            "sys.conversation_turns": 0,
            "sys.files": [],
            "sys.history": [],
            "sys.query": "",
            "sys.user_id": ""
        },
        "graph": {
            "edges": [
                {
                    "data": {"isHovered": False},
                    "id": "xy-edge__beginstart-Agent:StalePandasDreamend",
                    "source": "begin",
                    "sourceHandle": "start",
                    "target": "Agent:StalePandasDream",
                    "targetHandle": "end"
                },
                {
                    "data": {"isHovered": False},
                    "id": "xy-edge__Agent:StalePandasDreamstart-Message:BrownPugsStickend",
                    "source": "Agent:StalePandasDream",
                    "sourceHandle": "start",
                    "target": "Message:BrownPugsStick",
                    "targetHandle": "end"
                },
                {
                    "data": {"isHovered": False},
                    "hidden": False,
                    "id": "xy-edge__Agent:StalePandasDreamtool-Tool:PrettyMasksFloatend",
                    "selected": False,
                    "source": "Agent:StalePandasDream",
                    "sourceHandle": "tool",
                    "target": "Tool:PrettyMasksFloat",
                    "targetHandle": "end"
                }
            ],
            "nodes": [
                {
                    "data": {
                        "form": {
                            "enablePrologue": True,
                            "inputs": {},
                            "mode": "conversational",
                            "outputs": {},
                            "prologue": prologue_text
                        },
                        "label": "Begin",
                        "name": "begin"
                    },
                    "dragging": False,
                    "id": "begin",
                    "measured": {"height": 82, "width": 200},
                    "position": {"x": 47.5, "y": 199.5},
                    "selected": False,
                    "sourcePosition": "left",
                    "targetPosition": "right",
                    "type": "beginNode"
                },
                {
                    "data": {
                        "form": agent_params,
                        "label": "Agent",
                        "name": "QLTN Super Agent"
                    },
                    "dragging": False,
                    "id": "Agent:StalePandasDream",
                    "measured": {"height": 80, "width": 200},
                    "position": {"x": 351.5, "y": 231},
                    "selected": False,
                    "sourcePosition": "right",
                    "targetPosition": "left",
                    "type": "agentNode"
                },
                {
                    "data": {
                        "form": {
                            "content": ["{Agent:StalePandasDream@content}"],
                            "output_format": "markdown"
                        },
                        "label": "Message",
                        "name": "Message_0"
                    },
                    "dragging": False,
                    "id": "Message:BrownPugsStick",
                    "measured": {"height": 86, "width": 200},
                    "position": {"x": 671.5, "y": 192.5},
                    "selected": False,
                    "sourcePosition": "right",
                    "targetPosition": "left",
                    "type": "messageNode"
                },
                {
                    "data": {
                        "form": {
                            "description": "Bộ công cụ tích hợp Retrieval (CMS-DEMEGO + BA-DEMEGO) và MCP Server (QLTN-Sandbox-Mulberrylane).",
                            "user_prompt": ""
                        },
                        "label": "Tool",
                        "name": "flow.tool_0"
                    },
                    "dragging": False,
                    "hidden": False,
                    "id": "Tool:PrettyMasksFloat",
                    "measured": {"height": 82, "width": 200},
                    "position": {"x": 351.5, "y": 375.5},
                    "selected": False,
                    "sourcePosition": "right",
                    "targetPosition": "left",
                    "type": "toolNode"
                }
            ]
        },
        "history": [],
        "memory": [],
        "messages": [],
        "path": [],
        "retrieval": [],
        "variables": {
            "base_url": {
                "description": "Tên miền cơ sở dùng để đính kèm link trực tiếp tới màn hình nghiệp vụ cho người dùng",
                "name": "base_url",
                "type": "string",
                "value": "https://mulberrylane.sandbox.demego.vn"
            }
        }
    }

    payload = {
        "title": "Agent QLTN",
        "description": "Trợ lý AI thông minh cấp cao kết hợp toàn bộ Kho tri thức (CMS Demego + BA Demego) và Hệ thống MCP QLTN.",
        "permission": "team",
        "canvas_category": "agent_canvas",
        "dsl": dsl
    }

    r_create = requests.post(f"{RAGFLOW_API_BASE}/agents", headers=HEADERS, json=payload, timeout=30)
    print("CREATE AGENT STATUS:", r_create.status_code)
    res_json = r_create.json()
    if r_create.status_code == 200 and res_json.get("code") == 0:
        canvas_id = res_json["data"]["id"]
        print(f"✔ Đã tạo thành công Agent QLTN với ID: {canvas_id}")
        r_pub = requests.post(f"{RAGFLOW_API_BASE}/agents/{canvas_id}/publish", headers=HEADERS, json={}, timeout=15)
        print("PUBLISH STATUS:", r_pub.status_code, r_pub.text)
    else:
        print("❌ Lỗi khi tạo Agent:", r_create.text)

if __name__ == "__main__":
    main()
