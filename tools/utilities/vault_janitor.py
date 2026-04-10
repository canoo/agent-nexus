import os
import re
import google.generativeai as genai
import shutil
import time

# 1. Setup API
genai.configure(api_key=os.environ.get("GEMINI_API_KEY"))

# 2. Vault Paths
VAULT_PATH = os.path.expanduser("~/Documents/ObsidianVault/codelogiic")
ARCHIVE_PATH = os.path.join(VAULT_PATH, "archive", "chat_fragments")
os.makedirs(ARCHIVE_PATH, exist_ok=True)

# 3. The Synthesizer Prompt (Using Flash for the massive context window)
model = genai.GenerativeModel(
    model_name="gemini-2.5-flash",
    system_instruction="""
    You are an expert Technical Writer and Zettelkasten Architect. 
    I am giving you a messy, fragmented text dump of multiple AI chat logs and notes about a single topic.
    
    YOUR JOB: Synthesize this into ONE master Wiki-style guide.
    - Remove all repetitive prompts, duplicate answers, and AI conversational filler.
    - Organize the information logically with Markdown headers (##).
    - Combine code blocks or steps that belong together.
    - Make it a clean, highly readable, permanent reference document.
    """
)

def build_backlink_map():
    print("🗺️ Mapping vault backlinks...")
    parent_map = {}
    
    for root, dirs, files in os.walk(VAULT_PATH):
        # Skip the archive folder so we don't get stuck in a loop
        if "archive" in root:
            continue
            
        for file in files:
            if file.endswith(".md"):
                file_path = os.path.join(root, file)
                with open(file_path, 'r', encoding='utf-8') as f:
                    content = f.read()
                
                # Regex to find the exact Parent Concept
                match = re.search(r'- \*\*Parent Concept:\*\* \[\[(.*?)\]\]', content)
                if match:
                    parent_topic = match.group(1).strip()
                    # Ignore empty brackets
                    if parent_topic: 
                        if parent_topic not in parent_map:
                            parent_map[parent_topic] = []
                        parent_map[parent_topic].append(file_path)
                    
    return parent_map

def synthesize_topics(parent_map):
    # Only process topics that have 2 or more fragmented notes
    topics_to_process = {k: v for k, v in parent_map.items() if len(v) > 1}
    
    print(f"🎯 Found {len(topics_to_process)} topics that need consolidation.")
    
    for topic, file_paths in topics_to_process.items():
        print(f"🧹 Synthesizing {len(file_paths)} fragments for [[{topic}]]...")
        
        combined_text = ""
        for path in file_paths:
            with open(path, 'r', encoding='utf-8') as f:
                combined_text += f"\n\n--- Source Fragment ---\n"
                combined_text += f.read()
        
        try:
            # Ask Gemini to clean up the mess
            response = model.generate_content(combined_text)
            master_note_content = response.text.strip()
            
            # Create the new Master Note at the root of the vault
            # (You can drag and drop it into 1 Projects or 2 Areas later)
            master_path = os.path.join(VAULT_PATH, f"{topic.replace('/', '-')}.md")
            
            with open(master_path, 'w', encoding='utf-8') as f:
                f.write(master_note_content)
            
            # Move the messy fragments to the archive
            for path in file_paths:
                # Handle potential duplicate filenames in the archive
                base_name = os.path.basename(path)
                dest_path = os.path.join(ARCHIVE_PATH, base_name)
                if os.path.exists(dest_path):
                    dest_path = os.path.join(ARCHIVE_PATH, f"copy_{base_name}")
                shutil.move(path, dest_path)
            
            print(f"✅ Created Master Note: {topic}.md and archived fragments.")
            
            # 5-second pause to protect the API limits
            time.sleep(5)
            
        except Exception as e:
            print(f"⚠️ Error synthesizing {topic}: {e}")

if __name__ == "__main__":
    backlink_map = build_backlink_map()
    synthesize_topics(backlink_map)
    print("-" * 30)
    print("✨ Vault Janitor Complete! Check your root folder for the new Master Notes.")
