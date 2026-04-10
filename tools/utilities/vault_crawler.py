import os
import google.generativeai as genai
import time

# 1. Setup API and Paths
genai.configure(api_key=os.environ.get("GEMINI_API_KEY"))
# Pointing directly to your vault
VAULT_PATH = os.path.expanduser("~/Documents/ObsidianVault/codelogiic")

# 2. The Surgical "Tree Builder" Prompt
model = genai.GenerativeModel(
    model_name="gemini-2.5-flash-lite",
    system_instruction="""
    You are an expert Zettelkasten Librarian. You will be given the text of an existing Obsidian markdown note.
    
    YOUR ONLY JOB: Read the note and generate a "Conceptual Tree" to append to the bottom of it.
    
    CRITICAL RULES:
    1. DO NOT summarize, rewrite, or alter the original text.
    2. ONLY output the exact Markdown block requested below. No conversational filler.
    
    Output exactly this format and nothing else:
    
    ## Conceptual Tree
    - **Parent Concept:** [[Guess the broader root topic]]
    - **Related Topics:** [[Sibling concept 1]], [[Sibling concept 2]]
    - **Child Concepts:** [[Narrower sub-topics found in the text]]
    """
)

def crawl_and_retrofit():
    print(f"🕵️ Scanning vault: {VAULT_PATH}")
    
    processed = 0
    skipped = 0
    
    # os.walk crawls every folder and sub-folder
    for root, dirs, files in os.walk(VAULT_PATH):
        for file in files:
            if file.endswith(".md"):
                file_path = os.path.join(root, file)
                
                # Open the note and read it
                with open(file_path, 'r', encoding='utf-8') as f:
                    content = f.read()
                
                # THE SAFETY NET: Check if it already has a tree
                # This guarantees we skip your 1,005 new Takeout notes!
                if "## Conceptual Tree" in content:
                    skipped += 1
                    continue
                    
                print(f"🌲 Building tree for: {file}...")
                
                try:
                    # Send the old note to Gemini
                    response = model.generate_content(content)
                    new_tree = response.text.strip()
                    
                    # Append the new tree to the bottom of the old note safely
                    with open(file_path, 'a', encoding='utf-8') as f:
                        f.write("\n\n" + new_tree + "\n")
                    
                    processed += 1
                    
                    # 4-second pause to respect API rate limits
                    time.sleep(4)
                    
                except Exception as e:
                    print(f"⚠️ Error on {file}: {e}")

    print("-" * 30)
    print("✅ Vault Crawl Complete!")
    print(f"Notes Retrofitted: {processed}")
    print(f"Notes Skipped: {skipped}")

if __name__ == "__main__":
    crawl_and_retrofit()
