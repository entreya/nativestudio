// diff.js

// Inject modal HTML
const diffHtml = `
<div id="diff-overlay" class="hidden"></div>
<div id="diff-modal" class="hidden">
  <div class="diff-header">
    <span>AI Suggested Edit</span>
    <div class="diff-actions">
      <button id="accept-all" class="secondary">Accept All</button>
      <button id="reject-all" class="secondary">Reject All</button>
      <button id="close-diff" class="secondary">✕</button>
    </div>
  </div>
  <div class="diff-body" id="diff-body">
    <div class="diff-left" id="diff-left"></div>
    <div class="diff-right" id="diff-right"></div>
  </div>
  <div class="diff-footer">
    <span id="diff-stats"></span>
    <button id="apply-accepted">Apply Accepted Changes</button>
  </div>
</div>
`;
document.body.insertAdjacentHTML('beforeend', diffHtml);

// Toast container
const toastContainer = document.createElement('div');
toastContainer.id = 'toast-container';
document.body.appendChild(toastContainer);

// Elements
const modal = document.getElementById('diff-modal');
const overlay = document.getElementById('diff-overlay');
const closeBtn = document.getElementById('close-diff');
const acceptAllBtn = document.getElementById('accept-all');
const rejectAllBtn = document.getElementById('reject-all');
const applyBtn = document.getElementById('apply-accepted');
const leftPanel = document.getElementById('diff-left');
const rightPanel = document.getElementById('diff-right');
const diffStats = document.getElementById('diff-stats');

let currentDiffLines = [];
let currentFilepath = '';

// Sync scrolling
leftPanel.addEventListener('scroll', () => { 
    rightPanel.scrollTop = leftPanel.scrollTop; 
    rightPanel.scrollLeft = leftPanel.scrollLeft; 
});
rightPanel.addEventListener('scroll', () => { 
    leftPanel.scrollTop = rightPanel.scrollTop; 
    leftPanel.scrollLeft = rightPanel.scrollLeft; 
});

// Myers diff implementation (simple line-level LCS)
function computeDiff(original, suggested) {
    const origLines = original.split('\n');
    const suggLines = suggested.split('\n');
    
    // DP array for LCS
    const m = origLines.length;
    const n = suggLines.length;
    const dp = Array(m + 1).fill(null).map(() => Array(n + 1).fill(0));
    
    for (let i = 1; i <= m; i++) {
        for (let j = 1; j <= n; j++) {
            if (origLines[i-1] === suggLines[j-1]) {
                dp[i][j] = dp[i-1][j-1] + 1;
            } else {
                dp[i][j] = Math.max(dp[i-1][j], dp[i][j-1]);
            }
        }
    }
    
    // Backtrack to find diff
    let i = m, j = n;
    const diff = [];
    
    while (i > 0 || j > 0) {
        if (i > 0 && j > 0 && origLines[i-1] === suggLines[j-1]) {
            diff.push({ type: 'equal', content: origLines[i-1], origLine: i, suggLine: j });
            i--; j--;
        } else if (j > 0 && (i === 0 || dp[i][j-1] >= dp[i-1][j])) {
            diff.push({ type: 'insert', content: suggLines[j-1], suggLine: j, checked: true });
            j--;
        } else if (i > 0 && (j === 0 || dp[i][j-1] < dp[i-1][j])) {
            diff.push({ type: 'delete', content: origLines[i-1], origLine: i, checked: true });
            i--;
        }
    }
    
    return diff.reverse();
}

function renderDiff(original, suggested) {
    currentDiffLines = computeDiff(original, suggested);
    
    let leftHtml = '';
    let rightHtml = '';
    
    let origLineNum = 1;
    let suggLineNum = 1;
    
    let inserts = 0;
    let deletes = 0;

    currentDiffLines.forEach((line, index) => {
        const checkbox = `<input type="checkbox" class="line-cb" data-index="${index}" ${line.checked !== false ? 'checked' : ''}>`;
        
        if (line.type === 'equal') {
            leftHtml += `<div class="diff-line equal"><span class="line-num">${origLineNum++}</span><span class="line-content">${escapeHtml(line.content)}</span></div>`;
            rightHtml += `<div class="diff-line equal"><span class="line-num">${suggLineNum++}</span><span class="line-content">${escapeHtml(line.content)}</span></div>`;
        } else if (line.type === 'delete') {
            leftHtml += `<div class="diff-line delete" data-index="${index}"><span class="line-num">${origLineNum++}</span>${checkbox}<span class="line-content">${escapeHtml(line.content)}</span></div>`;
            rightHtml += `<div class="diff-line empty"><span class="line-num"></span><span class="line-content"> </span></div>`;
            deletes++;
        } else if (line.type === 'insert') {
            leftHtml += `<div class="diff-line empty"><span class="line-num"></span><span class="line-content"> </span></div>`;
            rightHtml += `<div class="diff-line insert" data-index="${index}"><span class="line-num">${suggLineNum++}</span>${checkbox}<span class="line-content">${escapeHtml(line.content)}</span></div>`;
            inserts++;
        }
    });

    leftPanel.innerHTML = leftHtml;
    rightPanel.innerHTML = rightHtml;
    diffStats.innerText = `+${inserts} -${deletes} lines`;
    
    // Add event listeners to checkboxes
    document.querySelectorAll('.line-cb').forEach(cb => {
        cb.addEventListener('change', (e) => {
            const idx = parseInt(e.target.getAttribute('data-index'));
            currentDiffLines[idx].checked = e.target.checked;
            updatePreview();
        });
    });
}

function updatePreview() {
    document.querySelectorAll('.diff-line.delete, .diff-line.insert').forEach(el => {
        const idx = el.getAttribute('data-index');
        const cb = el.querySelector('.line-cb');
        if (cb && !cb.checked) {
            el.classList.add('unchecked');
        } else {
            el.classList.remove('unchecked');
        }
    });
}

function escapeHtml(unsafe) {
    return (unsafe || '').replace(/&/g, "&amp;")
         .replace(/</g, "&lt;")
         .replace(/>/g, "&gt;")
         .replace(/"/g, "&quot;")
         .replace(/'/g, "&#039;");
}

function getAcceptedResult() {
    const result = [];
    currentDiffLines.forEach(line => {
        if (line.type === 'equal') {
            result.push(line.content);
        } else if (line.type === 'delete') {
            if (!line.checked) {
                // If we uncheck a delete, we KEEP the original line
                result.push(line.content);
            }
        } else if (line.type === 'insert') {
            if (line.checked) {
                // If we check an insert, we ADD the new line
                result.push(line.content);
            }
        }
    });
    return result.join('\n');
}

window.showDiff = function(originalCode, suggestedCode, filepath) {
    currentFilepath = filepath;
    renderDiff(originalCode, suggestedCode);
    modal.classList.remove('hidden');
    overlay.classList.remove('hidden');
};

function closeDiff() {
    modal.classList.add('hidden');
    overlay.classList.add('hidden');
}

closeBtn.addEventListener('click', closeDiff);
overlay.addEventListener('click', closeDiff);

acceptAllBtn.addEventListener('click', () => {
    currentDiffLines.forEach(l => { if (l.type !== 'equal') l.checked = true; });
    document.querySelectorAll('.line-cb').forEach(cb => cb.checked = true);
    updatePreview();
});

rejectAllBtn.addEventListener('click', () => {
    currentDiffLines.forEach(l => { if (l.type !== 'equal') l.checked = false; });
    document.querySelectorAll('.line-cb').forEach(cb => cb.checked = false);
    updatePreview();
});

applyBtn.addEventListener('click', async () => {
    const merged = getAcceptedResult();
    
    // Set Monaco editor value
    if (window.editor) {
        window.editor.setValue(merged);
    }
    
    // Auto-save
    if (currentFilepath) {
        try {
            await fetch('/api/file', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ path: currentFilepath, content: merged })
            });
            
            // Add to context (fire and forget)
            fetch('/api/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    prompt: `[System: User accepted code edit for ${currentFilepath}]`,
                    model: window.currentModel || 'codellama',
                    session_id: window.sessionId
                })
            });
            
            showToast("Changes applied ✓", "success");
        } catch (e) {
            showToast("Failed to save changes", "error");
        }
    }
    
    closeDiff();
});

window.showToast = function(message, type = 'success') {
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.innerText = message;
    toastContainer.appendChild(toast);
    
    setTimeout(() => toast.classList.add('show'), 10);
    setTimeout(() => {
        toast.classList.remove('show');
        setTimeout(() => toast.remove(), 300);
    }, 2500);
};
