/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

export interface GithubActionsWorkflowParams {
  repositoryOwner?: string;
  repositoryName?: string;
  branch?: string;
  folderPath?: string;
  baseUrl?: string;
}

export function buildGithubActionsWorkflowYaml({
  repositoryOwner,
  repositoryName,
  branch,
  folderPath,
  baseUrl,
}: GithubActionsWorkflowParams): string {
  const effectiveOwner = (repositoryOwner || '').trim() || 'your-org';
  const effectiveRepo =
    (repositoryName || '').split(',')[0]?.trim() || 'your-repo';
  const effectiveBranch = (branch || '').trim() || 'main';
  const normalizedFolder = (folderPath || '')
    .trim()
    .replace(/^\/+|\/+$/g, '');
  const targetFolder = normalizedFolder || 'docs';
  const effectiveBaseUrl =
    (baseUrl || '').trim().replace(/\/+$/, '') ||
    'https://your-ragflow-domain.com';

  return `name: Sync Docs to RAGFlow Dataset (${effectiveOwner}/${effectiveRepo})

on:
  push:
    branches:
      - ${effectiveBranch}
    paths:
      - '${targetFolder}/**'
  workflow_dispatch:

env:
  RAGFLOW_BASE_URL: '${effectiveBaseUrl}'
  TARGET_FOLDER: '${targetFolder}'

jobs:
  sync-docs-to-ragflow:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Sync & Parse documents in RAGFlow
        env:
          RAGFLOW_API_KEY: \${{ secrets.RAGFLOW_API_KEY }}
          RAGFLOW_DATASET_ID: \${{ secrets.RAGFLOW_DATASET_ID }}
        run: |
          set -euo pipefail

          if [ -z "\${RAGFLOW_API_KEY:-}" ] || [ -z "\${RAGFLOW_DATASET_ID:-}" ]; then
            echo "Error: RAGFLOW_API_KEY and RAGFLOW_DATASET_ID secrets must be configured."
            exit 1
          fi

          UPLOADED_DOC_IDS=()

          while IFS= read -r -d '' FILE_PATH; do
            REL_PATH="\${FILE_PATH#\${TARGET_FOLDER}/}"
            DOC_NAME="$(echo "\${REL_PATH}" | tr '/' '_')"
            echo "Syncing \${FILE_PATH} as \${DOC_NAME}..."

            # 1. Find and remove existing document with the same name (Idempotent Upsert)
            EXISTING_IDS=$(curl -sS -G "\${RAGFLOW_BASE_URL}/api/v1/datasets/\${RAGFLOW_DATASET_ID}/documents" \\
              -H "Authorization: Bearer \${RAGFLOW_API_KEY}" \\
              --data-urlencode "keywords=\${DOC_NAME}" \\
              | jq -r --arg NAME "\${DOC_NAME}" '.data.docs[]? | select(.name == $NAME) | .id')

            if [ -n "\${EXISTING_IDS}" ]; then
              DELETE_PAYLOAD=$(printf '%s\\n' "\${EXISTING_IDS}" | jq -R . | jq -s '{ids: .}')
              curl -sS -X DELETE "\${RAGFLOW_BASE_URL}/api/v1/datasets/\${RAGFLOW_DATASET_ID}/documents" \\
                -H "Authorization: Bearer \${RAGFLOW_API_KEY}" \\
                -H "Content-Type: application/json" \\
                -d "\${DELETE_PAYLOAD}" > /dev/null
            fi

            # 2. Upload latest document version
            UPLOAD_RESP=$(curl -sS -X POST "\${RAGFLOW_BASE_URL}/api/v1/datasets/\${RAGFLOW_DATASET_ID}/documents" \\
              -H "Authorization: Bearer \${RAGFLOW_API_KEY}" \\
              -F "file=@\${FILE_PATH};filename=\${DOC_NAME}")

            NEW_DOC_ID=$(echo "\${UPLOAD_RESP}" | jq -r '.data[0].id // empty')
            if [ -n "\${NEW_DOC_ID}" ]; then
              UPLOADED_DOC_IDS+=("\${NEW_DOC_ID}")
            else
              echo "Upload failed for \${FILE_PATH}: \${UPLOAD_RESP}"
              exit 1
            fi
          done < <(find "\${TARGET_FOLDER}" -type f \\( -name "*.md" -o -name "*.mdx" -o -name "*.txt" -o -name "*.pdf" -o -name "*.docx" -o -name "*.yaml" -o -name "*.yml" -o -name "*.json" \\) -print0)

          # 3. Trigger chunk parsing (Indexing) for all uploaded documents
          if [ \${#UPLOADED_DOC_IDS[@]} -gt 0 ]; then
            PARSE_PAYLOAD=$(printf '%s\\n' "\${UPLOADED_DOC_IDS[@]}" | jq -R . | jq -s '{document_ids: .}')
            curl -sS -X POST "\${RAGFLOW_BASE_URL}/api/v1/datasets/\${RAGFLOW_DATASET_ID}/chunks" \\
              -H "Authorization: Bearer \${RAGFLOW_API_KEY}" \\
              -H "Content-Type: application/json" \\
              -d "\${PARSE_PAYLOAD}" > /dev/null
            echo "Successfully synced and triggered indexing for \${#UPLOADED_DOC_IDS[@]} document(s)."
          fi
`;
}
