let editor;
window.activeFilePath = null;
let isUnsaved = false;

// UI Elements
const fileTreeEl = document.getElementById('file-tree-content');
const statusPathEl = document.getElementById('status-path');
const statusUnsavedEl = document.getElementById('status-unsaved');

// Initialize Monaco
require(['vs/editor/editor.main'], function () {
    editor = monaco.editor.create(document.getElementById('monaco-container'), {
        value: '// Select a file from the left to start editing\n',
        language: 'javascript',
        theme: 'vs', // Light theme
        automaticLayout: true,
        minimap: { enabled: false },
        fontSize: 14,
        fontFamily: "var(--font-mono)",
        padding: { top: 15 }
    });

    editor.onDidChangeModelContent(() => {
        if (window.activeFilePath && !isUnsaved) {
            isUnsaved = true;
            statusUnsavedEl.style.display = 'inline';
        }
    });

    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, function() {
        saveFile();
    });

    const openBtn = document.getElementById('btn-open-folder');
    if (openBtn) {
        openBtn.addEventListener('click', async () => {
            const newPath = prompt("Enter the absolute path to the project directory:\n(e.g., /Users/name/projects/my-app)");
            if (!newPath) return;
            
            try {
                const res = await fetch('/api/workspace', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ path: newPath })
                });
                
                if (!res.ok) {
                    alert("Invalid directory or permission denied.");
                    return;
                }
                
                // Reset active file and editor
                window.activeFilePath = null;
                document.getElementById('status-path').innerText = "No file open";
                if (window.editor) window.editor.setValue("// Select a file from the left to start editing\n");
                
                // Reload tree
                await fetchFileTree();
            } catch (e) {
                console.error(e);
            }
        });
    }

    fetchFileTree();
});

// ----------------- FILE SYSTEM -----------------

async function fetchFileTree() {
    try {
        const res = await fetch('/api/files');
        const files = await res.json();
        fileTreeEl.innerHTML = '';
        renderTree(files, fileTreeEl);
    } catch (e) {
        console.error("Failed to fetch file tree", e);
    }
}

function renderTree(nodes, parentEl) {
    if (!nodes) return;
    nodes.forEach(node => {
        const div = document.createElement('div');
        div.className = 'file-node';
        div.innerHTML = `<span class="file-icon">${node.type === 'dir' ? '📁' : '📄'}</span> <span>${node.name}</span>`;
        
        const childrenContainer = document.createElement('div');
        childrenContainer.className = 'file-node-children';
        childrenContainer.style.display = 'none';

        if (node.type === 'dir') {
            div.onclick = (e) => {
                e.stopPropagation();
                const isHidden = childrenContainer.style.display === 'none';
                childrenContainer.style.display = isHidden ? 'block' : 'none';
                div.querySelector('.file-icon').innerText = isHidden ? '📂' : '📁';
            };
            renderTree(node.children, childrenContainer);
        } else {
            div.onclick = (e) => {
                e.stopPropagation();
                document.querySelectorAll('.file-node').forEach(el => el.classList.remove('active'));
                div.classList.add('active');
                openFile(node.path);
            };
        }

        parentEl.appendChild(div);
        if (node.type === 'dir') {
            parentEl.appendChild(childrenContainer);
        }
    });
}

function getExtLanguage(ext) {
    const map = {
        '.php': 'php', '.go': 'go', '.js': 'javascript', '.ts': 'typescript',
        '.py': 'python', '.md': 'markdown', '.json': 'json', '.yaml': 'yaml',
        '.html': 'html', '.css': 'css'
    };
    return map[ext] || 'plaintext';
}

async function openFile(path) {
    try {
        const res = await fetch(`/api/file?path=${encodeURIComponent(path)}`);
        if (!res.ok) throw new Error("Failed to load file");
        const data = await res.json();
        
        window.activeFilePath = data.path;
        statusPathEl.innerText = window.activeFilePath;
        isUnsaved = false;
        statusUnsavedEl.style.display = 'none';

        const ext = path.substring(path.lastIndexOf('.'));
        const lang = getExtLanguage(ext);

        monaco.editor.setModelLanguage(editor.getModel(), lang);
        editor.setValue(data.content);
    } catch (e) {
        console.error(e);
    }
}

async function saveFile() {
    if (!window.activeFilePath) return;
    try {
        const content = editor.getValue();
        const res = await fetch('/api/file', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: window.activeFilePath, content })
        });
        if (res.ok) {
            isUnsaved = false;
            statusUnsavedEl.style.display = 'none';
        }
    } catch (e) {
        console.error("Save failed", e);
    }
}

window.getSelectedText = function() {
    if (!editor) return "";
    return editor.getModel().getValueInRange(editor.getSelection());
};

window.getCurrentContent = function() {
    if (!editor) return "";
    return editor.getValue();
};
