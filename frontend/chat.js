window.pendingCodeBlocks = [];
let sessionId = generateSessionId();
let currentModel = "codellama";

// DOM Elements
const modelSelect = document.getElementById('model-select');
const ctxProgress = document.getElementById('context-progress');
const ctxPct = document.getElementById('context-pct');
const ctxZone = document.getElementById('context-zone');
const resetBtn = document.getElementById('reset-btn');
const chatMessages = document.getElementById('chat-messages');
const chatInput = document.getElementById('chat-input');
const sendBtn = document.getElementById('send-btn');
const sendFileChk = document.getElementById('send-file-chk');

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    loadModels();
    refreshContext();

    modelSelect.addEventListener('change', () => {
        currentModel = modelSelect.value;
        refreshContext();
    });

    resetBtn.addEventListener('click', resetContext);

    sendBtn.addEventListener('click', () => {
        sendMessage(chatInput.value);
    });

    chatInput.addEventListener('keydown', (e) => {
        if (e.ctrlKey && e.key === 'Enter') {
            e.preventDefault();
            sendMessage(chatInput.value);
        }
    });
});

function generateSessionId() {
    if (window.crypto && window.crypto.randomUUID) {
        return crypto.randomUUID();
    }
    return Math.random().toString(36).substring(2, 10);
}

async function loadModels() {
    try {
        const res = await fetch('/api/models');
        const data = await res.json();
        if (data.models && data.models.length > 0) {
            modelSelect.innerHTML = '';
            data.models.forEach(m => {
                const opt = document.createElement('option');
                opt.value = m;
                opt.innerText = m;
                modelSelect.appendChild(opt);
            });
            currentModel = data.models[0];
            modelSelect.value = currentModel;
        }
    } catch (e) {
        console.error("Failed to load models", e);
    }
}

async function refreshContext() {
    try {
        const res = await fetch(`/api/context?session_id=${sessionId}&model=${currentModel}`);
        const data = await res.json();
        
        const pct = Math.round(data.usage_pct * 100);
        ctxPct.innerText = `${pct}%`;
        ctxProgress.style.width = `${Math.min(pct, 100)}%`;
        
        const zone = data.zone || 'green';
        ctxZone.innerText = zone.charAt(0).toUpperCase() + zone.slice(1);
        
        const colorVar = `var(--${zone})`;
        ctxProgress.style.backgroundColor = colorVar;
        ctxZone.style.color = colorVar;

    } catch (e) {
        console.error("Context fetch failed", e);
    }
}

async function resetContext() {
    try {
        await fetch('/api/context/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: sessionId })
        });
        chatMessages.innerHTML = '<div class="msg system">Session reset. Context cleared.</div>';
        sessionId = generateSessionId(); 
        refreshContext();
    } catch (e) {
        console.error("Failed to reset context", e);
    }
}

function renderMarkdownText(text) {
    let html = text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    
    // Bold
    html = html.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    
    // Code blocks
    html = html.replace(/```([\w]*)\n([\s\S]*?)```/g, (match, lang, code) => {
        return `<pre><code class="language-${lang.trim()}">${code}</code></pre>`;
    });

    // Inline code
    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');

    // Line breaks
    html = html.replace(/\n/g, '<br>');

    return html;
}

function appendBubble(role, content) {
    const div = document.createElement('div');
    div.className = `msg ${role}`;
    
    if (role === 'assistant') {
        div.innerHTML = renderMarkdownText(content);
    } else {
        div.innerText = content;
    }

    chatMessages.appendChild(div);
    chatMessages.scrollTop = chatMessages.scrollHeight;
    return div;
}

function appendTypingIndicator() {
    const div = document.createElement('div');
    div.className = 'msg assistant typing-indicator';
    div.innerHTML = '<span>.</span><span>.</span><span>.</span>';
    chatMessages.appendChild(div);
    chatMessages.scrollTop = chatMessages.scrollHeight;
    return div;
}

async function sendMessage(promptText) {
    const text = promptText.trim();
    if (!text) return;

    let finalPrompt = text;
    
    if (sendFileChk.checked && window.activeFilePath && typeof window.getCurrentContent === 'function') {
        const content = window.getCurrentContent();
        finalPrompt += `\n\nCurrent file (${window.activeFilePath}):\n\`\`\`\n${content}\n\`\`\``;
    } else if (typeof window.getSelectedText === 'function') {
        const selected = window.getSelectedText();
        if (selected) {
            finalPrompt += `\n\n\`\`\`\n${selected}\n\`\`\``;
        }
    }

    chatInput.value = '';
    appendBubble('user', text);
    
    const typingIndicator = appendTypingIndicator();
    chatInput.disabled = true;
    sendBtn.disabled = true;

    try {
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                prompt: finalPrompt,
                model: currentModel,
                session_id: sessionId
            })
        });

        typingIndicator.remove();

        if (response.status === 429) {
            appendBubble('system', "Error: Context full. Summarizing...");
            refreshContext();
            return;
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder("utf-8");
        
        let assistantText = "";
        const assistantBubble = document.createElement('div');
        assistantBubble.className = 'msg assistant';
        chatMessages.appendChild(assistantBubble);
        
        while (true) {
            const { value, done } = await reader.read();
            if (done) break;
            
            const chunk = decoder.decode(value, { stream: true });
            const lines = chunk.split('\n\n');
            
            for (let line of lines) {
                if (line.startsWith('data: ')) {
                    const dataStr = line.substring(6);
                    try {
                        const data = JSON.parse(dataStr);
                        if (data.done) break;
                        if (data.token) {
                            assistantText += data.token;
                            assistantBubble.innerHTML = renderMarkdownText(assistantText);
                            chatMessages.scrollTop = chatMessages.scrollHeight;
                        }
                    } catch (e) {}
                }
            }
        }
        
        assistantBubble.innerHTML = renderMarkdownText(assistantText);
        parseResponseForCode(assistantText, assistantBubble);
        
        refreshContext();
    } catch (e) {
        typingIndicator.remove();
        appendBubble('system', "Error connecting to backend.");
    } finally {
        chatInput.disabled = false;
        sendBtn.disabled = false;
        chatInput.focus();
    }
}

function parseResponseForCode(text, bubbleEl) {
    const codeBlockRegex = /```[\w]*\n([\s\S]*?)```/g;
    let match;
    let foundBlocks = [];
    
    while ((match = codeBlockRegex.exec(text)) !== null) {
        foundBlocks.push(match[1].trim());
    }
    
    if (foundBlocks.length > 0) {
        window.pendingCodeBlocks = foundBlocks;
        
        const btn = document.createElement('button');
        btn.innerText = 'Apply changes?';
        btn.className = 'apply-changes-btn';
        btn.onclick = () => {
            if (window.activeFilePath && window.getCurrentContent) {
                const originalCode = window.getCurrentContent();
                const suggestedCode = window.pendingCodeBlocks[0]; 
                if (window.showDiff) {
                    window.showDiff(originalCode, suggestedCode, window.activeFilePath);
                }
            } else {
                alert("Please open a file first to apply changes.");
            }
        };
        
        bubbleEl.appendChild(btn);
    }
}
