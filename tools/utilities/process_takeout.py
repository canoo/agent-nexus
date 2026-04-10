import json
import os
import google.generativeai as genai
import time

# Get API key from the terminal environment variable
genai.configure(api_key=os.environ.get("GEMINI_API_KEY"))

# Your exact vault path
VAULT_PATH = os.path.expanduser("~/Documents/ObsidianVault/codelogiic")

# The path to your unzipped Takeout file
TAKEOUT_FILE = os.path.expanduser("~/Downloads/MyActivity.json") 

# Your updated vault structure
VAULT_FOLDERS = """
- 0 Inbox
- 1 Projects
- 1 Projects/Side Hustles
- 2 Areas/Career & Development/Software Development
- 2 Areas/Finances
- 2 Areas/Health & Wellness
- 2 Areas/Household
- 2 Areas/Language
- 2 Areas/Personal/Journal
- 2 Areas/Personal/Life Log
- 2 Areas/Personal/Relationships
- 2 Areas/Vehicles
- 3 Resources/Gaming
- Coral Island
"""

# The Second Brain Architect Prompt (Optimized for Trees and Synthesis)
model = genai.GenerativeModel(
    model_name="gemini-2.5-flash-lite",
    system_instruction=f"""
    You are an expert Personal Knowledge Management (PKM) Architect. Transform raw, messy chat transcripts into highly structured, Zettelkasten-style Obsidian wiki pages.
    
    CRITICAL RULES:
    1. ZERO CONVERSATION: Never use phrases like "The user asked", "The AI replied", or "Here is the summary". Write strictly as authoritative documentation.
    2. APPLY PROGRESSIVE SUMMARIZATION: Use a Layer 4 Executive Summary at the top, and strictly use **bolding** for critical concepts (Layer 2/3) throughout the text.
    3. If the chat is empty meta-data, a conversational dead-end, or an aborted prompt with no resolution, set "skip" to true.
    
    You MUST format the "content" string using this exact Markdown template:
    
    **TL;DR**
    [Write a clear, 2-3 sentence executive summary of the core problem, lesson, or idea. (Layer 4)]
    
    ---
    
    ## Theory & Context
    [Provide the technical background, architectural rules, or narrative context. Include relevant code snippets or terminal commands. Use **bold text** to highlight critical concepts.]
    
    ## Practice & Troubleshooting
    [List the actual steps taken, common mishaps, errors encountered, and the specific solutions. Format as a developer journal or step-by-step guide.]
    
    ## Conceptual Tree
    - **Parent Concept:** [[Guess the broader root topic, e.g., Backend, Hardware, Finances, Web Development]]
    - **Related Topics:** [[Sibling concept 1]], [[Sibling concept 2]]
    - **Child Concepts:** [[Narrower sub-topics discussed in this specific note]]
    
    Return a JSON object with these exact keys:
    - "title": A clean filename (no special characters).
    - "folder": The best matching folder from this list: {VAULT_FOLDERS}. If it doesn't fit, use "0 Inbox".
    - "tags": A list of 3-5 relevant Obsidian tags.
    - "content": The formatted markdown note using the strict template above.
    - "skip": Set to true ONLY if the chat is useless or a dead-end. Otherwise false.
    """
)

def process_chats():
    print("Opening Takeout data...")
    try:
        with open(TAKEOUT_FILE, 'r', encoding='utf-8') as f:
            data = json.load(f)
    except FileNotFoundError:
        print(f"❌ Could not find {TAKEOUT_FILE}. Did you unzip the Takeout folder into ~/Downloads?")
        return

    print(f"Found {len(data)} total chats. Processing the first 5 for testing...")
    
    # Process only the first 5 for our test run!
    for chat in data[:1400]:
        try:
            raw_text = str(chat) 
            
            print("Architecting note with Gemini...")
            response = model.generate_content(
                raw_text,
                generation_config=genai.GenerationConfig(response_mime_type="application/json")
            )
            
            result = json.loads(response.text)
            
            if result.get("skip"):
                print("⏭️ Chat was useless or empty. Skipping.")
                continue
                
            folder_path = os.path.join(VAULT_PATH, result["folder"])
            os.makedirs(folder_path, exist_ok=True)
            
            file_path = os.path.join(folder_path, f"{result['title']}.md")
            
            # Format the final Obsidian Note with YAML Frontmatter
            note_content = f"""---
tags: [{', '.join(result['tags'])}]
date_imported: {time.strftime('%Y-%m-%d')}
---
# {result['title']}

{result['content']}
"""
            with open(file_path, 'w', encoding='utf-8') as md_file:
                md_file.write(note_content)
                
            print(f"✅ Created: {result['folder']}/{result['title']}.md")
            
            # Pause for 4 seconds to respect the Flash-Lite 15 RPM free tier limit
            time.sleep(4) 
            
        except Exception as e:
            print(f"⚠️ Error processing a chat: {e}")

if __name__ == "__main__":
    process_chats()
