#!/usr/bin/env python3
"""Generate per-item menu photos for the dev seed catalog.

Each of the 150 menu items used to share its store's single representative
photo (see scripts/dev/gen-seed.py). This script renders a dedicated photo for
every item with the Bonsai-Image text-to-image model so each dish shows the
correct food, while keeping a consistent look within each store (same vessel /
background / lighting per store).

Usage:
    # render every item that does not yet have its own photo (default 1024x1024)
    python3 scripts/dev/gen-item-images.py

    # re-render everything, or a subset, or a different size
    python3 scripts/dev/gen-item-images.py --force
    python3 scripts/dev/gen-item-images.py --only i002,i093 --size 512x512

Requires a local checkout of the Bonsai-Image demo (the generator CLI):
    BONSAI_DIR=~/Documents/GitHub/Bonsai-Image-Demo   (default)
    -> $BONSAI_DIR/scripts/generate.sh --size WxH --prompt "..." --output out.png

Output: apps/employee/static/brand/items/<itemId>.jpg (matches seed-p2.sql).

The 10 store representative items (i001, i016, i031, i046, i061, i076, i091,
i107, i121, i136) already ship hand-picked photos in the MVP asset package and
are skipped by default. Pass --force --include-reps to overwrite them too.
"""

import argparse
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
ITEMS_DIR = os.path.join(REPO, "apps", "employee", "static", "brand", "items")
BONSAI_DIR = os.path.expanduser(
    os.environ.get("BONSAI_DIR", "~/Documents/GitHub/Bonsai-Image-Demo")
)

# Store representative items already shipped with the MVP asset package.
REP_ITEMS = {
    "i001", "i016", "i031", "i046", "i061",
    "i076", "i091", "i107", "i121", "i136",
}

# Per-store style suffix. Every item in a store appends the same suffix so the
# 15 photos share one visual language (vessel cue is per item, but background /
# lighting / treatment are fixed per store). Keyed by store number (r00N).
STORE_STYLE = {
    1: ", top-down 45 degree angle, soft natural light, warm wooden table "
       "background, appetizing menu hero shot, high detail",
    2: ", top-down 45 degree angle, soft warm light, rustic dark wooden table "
       "background, traditional Taiwanese eatery style, appetizing menu hero "
       "shot, high detail",
    3: ", top-down 45 degree angle, bright morning light, light wooden table "
       "background, cozy breakfast diner style, appetizing menu hero shot, "
       "high detail",
    4: ", 45 degree angle, bright airy natural light, white marble table "
       "background, modern brunch cafe style, appetizing menu hero shot, "
       "high detail",
    5: ", centered front view, soft studio light, clean light marble "
       "background, beverage product shot, vibrant and appetizing, high detail",
    6: ", top-down 45 degree angle, soft bright light, pastel light "
       "background, refreshing dessert style, appetizing menu hero shot, "
       "high detail",
    7: ", top-down 45 degree angle, moody warm light, dark wooden table "
       "background, Japanese ramen shop style, steamy and appetizing, "
       "high detail",
    8: ", top-down 45 degree angle, warm moody light, dark slate table "
       "background, Korean restaurant style, appetizing menu hero shot, "
       "high detail",
    9: ", 45 degree angle, warm light, rustic wooden board background, "
       "American diner style, appetizing menu hero shot, high detail",
    10: ", top-down 45 degree angle, warm natural light, rustic wooden table "
        "background, Italian trattoria style, appetizing menu hero shot, "
        "high detail",
}

# Per-item dish description (the "what's in the photo" part). The store style
# suffix above is appended automatically. Authored from the Chinese item names
# and descriptions in data/tw_food_delivery_mock_data_mvp.json.
DISHES = {
    # --- r001 阿城炙燒便當 (Taiwanese bento) ---
    "i002": "a Taiwanese fried pork chop bento, crispy marinated pork loin cutlet with a braised egg, pickled vegetables and white rice in a partitioned white takeout box",
    "i003": "a Taiwanese Sichuan pepper chicken bento, crispy fried chicken thigh in spicy numbing sauce with cilantro, white rice and greens in a partitioned white takeout box",
    "i004": "a Taiwanese braised pork belly bento, glossy soy-braised pork belly slices with pickled mustard greens, white rice and greens in a partitioned white takeout box",
    "i005": "a Taiwanese sliced pork belly bento with garlic soy sauce, thin boiled pork belly slices, white rice and greens in a partitioned white takeout box",
    "i006": "a Taiwanese salt-grilled mackerel bento, grilled mackerel fillet with a lemon wedge, white rice and greens in a partitioned white takeout box",
    "i007": "a Taiwanese three-cup king oyster mushroom bento, braised king oyster mushrooms with basil and garlic, white rice and greens in a partitioned white takeout box",
    "i008": "a Taiwanese combo bento with a crispy fried chicken cutlet and a grilled Taiwanese sausage, white rice and greens in a partitioned white takeout box",
    "i009": "a single grilled glazed Taiwanese chicken leg on a white plate",
    "i010": "a single crispy fried Taiwanese pork chop cutlet on a white plate",
    "i011": "a bowl of clear Taiwanese daily soup with vegetables and pork in a simple white bowl",
    "i012": "two Taiwanese soy-braised eggs in a small white dish",
    "i013": "a plate of blanched green vegetables drizzled with sauce and fried shallots on a white plate",
    "i014": "a small bowl of Taiwanese golden pickled cabbage",
    "i015": "a set of several Taiwanese bento boxes for a meeting, multiple partitioned takeout boxes with chicken, pork and rice arranged together",
    # --- r002 巷口滷肉飯研究室 (Taiwanese rice & small eats) ---
    "i017": "a bowl of Taiwanese braised pork belly rice, a thick glossy soy-braised pork belly slab over white rice with pickled greens, in a rustic ceramic bowl",
    "i018": "a bowl of Taiwanese shredded chicken rice, tender shredded chicken over white rice drizzled with chicken oil, in a rustic ceramic bowl",
    "i019": "a bowl of Taiwanese minced pork rice with pickled cucumber over white rice, in a rustic ceramic bowl",
    "i020": "a bowl of Taiwanese rice topped with both minced braised pork and shredded chicken, in a rustic ceramic bowl",
    "i021": "a bowl of vegetarian Taiwanese braised shiitake mushroom minced sauce over white rice, in a rustic ceramic bowl",
    "i022": "a bowl of Taiwanese qie-a noodle soup with sliced pork and scallions, in a rustic ceramic bowl",
    "i023": "a bowl of Taiwanese pork meatball soup with celery, in a rustic ceramic bowl",
    "i024": "a bowl of Taiwanese si-shen herbal pork soup, in a rustic ceramic bowl",
    "i025": "a bowl of Taiwanese clam and winter melon soup, in a rustic ceramic bowl",
    "i026": "a small plate of Taiwanese soy-braised tofu slices with scallions",
    "i027": "a small bowl of Taiwanese braised napa cabbage",
    "i028": "a plate of Taiwanese sliced pork cheek meat with garlic soy sauce",
    "i029": "a bowl of Sichuan-style wontons in red chili oil, in a rustic ceramic bowl",
    "i030": "a Taiwanese single-person set meal, a bowl of braised pork rice with a side soup and a small side dish on a rustic wooden tray",
    # --- r003 永春豆漿早餐鋪 (Taiwanese breakfast) ---
    "i032": "a Taiwanese sesame flatbread (shaobing) wrapped around a fried dough stick (youtiao) on a plate",
    "i033": "a Taiwanese egg crepe roll (dan bing) with bacon, sliced on a plate with sauce",
    "i034": "a Taiwanese egg crepe roll with sweet corn and egg, sliced on a plate",
    "i035": "a Taiwanese egg crepe roll with tuna and egg, sliced on a plate",
    "i036": "pan-fried Taiwanese radish cake with a fried egg on a plate with soy sauce",
    "i037": "a Taiwanese sticky rice ball (fantuan) filled with youtiao, pork floss and pickled radish, cut in half on a plate",
    "i038": "a Taiwanese purple rice vegetarian rice ball cut in half on a plate",
    "i039": "a Taiwanese brown sugar steamed bun filled with a fried egg on a plate",
    "i040": "a bamboo steamer basket of Taiwanese soup dumplings (xiaolongbao)",
    "i041": "a glass of iced Taiwanese soy milk with a straw",
    "i042": "a cup of hot Taiwanese rice and peanut milk (mijiang)",
    "i043": "a bowl of Taiwanese savory soy milk with youtiao bits, dried shrimp and scallions",
    "i044": "a glass of Taiwanese black tea soy milk with a straw",
    "i045": "a Taiwanese breakfast set for two, egg crepes, a rice ball and two cups of soy milk on a tray",
    # --- r004 晨光吐司研究所 (brunch / toast / coffee) ---
    "i047": "a thick toast sandwich with truffle scrambled egg, on a wooden board",
    "i048": "a thick toast sandwich with charcoal-grilled chicken thigh and lettuce, on a wooden board",
    "i049": "a thick toast sandwich with taro paste and pork floss, on a wooden board",
    "i050": "a thick toast sandwich with a hash brown, cheese and a fried egg, on a wooden board",
    "i051": "a toast sandwich with tuna and sweet corn salad, on a wooden board",
    "i052": "a brunch plate with pan-seared chicken breast over fresh salad greens, on a white plate",
    "i053": "a brunch plate with avocado, smoked salmon and a poached egg, on a white plate",
    "i054": "a vegetarian brunch plate with sauteed wild mushrooms and vegetables, on a white plate",
    "i055": "a kids brunch plate with mini pancakes, fruit and a small egg, on a white plate",
    "i056": "a glass of iced americano coffee with ice cubes",
    "i057": "a cup of oat milk latte with latte art",
    "i058": "a glass of sparkling honey lemon soda with lemon slices and ice",
    "i059": "a glass of fresh milk tea with ice",
    "i060": "a weekend brunch set for two, toast sandwiches, salad plates and two coffees on a marble table",
    # --- r005 茶里王手作茶飲 (bubble tea / drinks) ---
    "i062": "Tieguanyin oolong milk tea in a clear plastic cup with a sealed dome lid and a straw",
    "i063": "four-seasons spring green tea in a clear plastic cup with a sealed lid and a straw, light golden color",
    "i064": "aged black tea in a clear plastic cup with a sealed lid and a straw, deep amber color",
    "i065": "jasmine green tea in a clear plastic cup with a sealed lid and a straw, light green color",
    "i066": "winter melon green tea in a clear plastic cup with a sealed lid and a straw, amber color",
    "i067": "classic bubble milk tea with black tapioca pearls in a clear plastic cup with a sealed dome lid and a wide straw",
    "i068": "black tea latte with a layer of fresh milk in a clear plastic cup with a sealed lid and a straw",
    "i069": "taro fresh milk with sago pearls in a clear plastic cup with a sealed lid and a wide straw, purple color",
    "i070": "matcha green tea fresh milk in a clear plastic cup with a sealed lid and a straw, layered green and white",
    "i071": "passion fruit green tea with aiyu jelly in a clear plastic cup with a sealed lid and a wide straw, orange color",
    "i072": "grapefruit green tea with grapefruit pulp in a clear plastic cup with a sealed lid and a straw",
    "i073": "lemon aiyu jelly drink with ice in a clear plastic cup with a sealed lid and a wide straw",
    "i074": "a mango smoothie slush in a clear plastic cup with a dome lid and a wide straw, topped with mango",
    "i075": "a drink carrier tray with six assorted Taiwanese bubble tea drinks in clear plastic cups with sealed lids",
    # --- r006 島嶼豆花甜品 (douhua / dessert) ---
    "i077": "Taiwanese tofu pudding (douhua) with brown sugar fenguo jelly cubes in sweet syrup, in a white bowl",
    "i078": "Taiwanese tofu pudding with stewed peanuts in sweet syrup, in a white bowl",
    "i079": "Taiwanese tofu pudding with chewy taro balls in sweet syrup, in a white bowl",
    "i080": "Taiwanese grass jelly milk pudding dessert in a glass cup",
    "i081": "a warm bowl of Taiwanese red bean and barley sweet soup, in a white bowl",
    "i082": "Taiwanese mango shaved snow ice topped with fresh mango cubes and condensed milk, in a bowl",
    "i083": "Taiwanese shaved snow ice with peanut and mochi, in a bowl",
    "i084": "Uji matcha and red bean shaved ice with mochi, in a bowl",
    "i085": "Taiwanese lemon aiyu jelly with ice in a glass cup",
    "i086": "a glass of iced winter melon lemon tea",
    "i087": "a glass of iced grass jelly herbal tea",
    "i088": "a glass of red bean fresh milk with ice",
    "i089": "a glass of cold-brew osmanthus oolong tea",
    "i090": "a sharing set of three Taiwanese desserts, tofu pudding, shaved ice and grass jelly, on a tray",
    # --- r007 東京一番拉麵屋 (Japanese ramen / donburi) ---
    "i092": "Japanese karaage fried chicken over curry rice on a plate",
    "i093": "Japanese miso corn ramen with chashu pork, scallions and a soft egg in a dark ceramic bowl",
    "i094": "Japanese spicy miso ramen with chili oil, chashu pork and scallions in a dark ceramic bowl",
    "i095": "Japanese shoyu chicken ramen with sliced chicken and scallions in a dark ceramic bowl",
    "i096": "Japanese shio vegetable ramen with assorted vegetables in a clear broth in a dark ceramic bowl",
    "i097": "a Japanese seared chashu pork rice bowl (donburi) with scallions and a soft egg",
    "i098": "a Japanese oyakodon chicken and egg rice bowl with scallions",
    "i099": "a Japanese thin-sliced beef rice bowl (gyudon) with onions and scallions",
    "i100": "a Japanese glazed grilled sea bream rice bowl",
    "i101": "Japanese pan-fried gyoza dumplings on a plate with dipping sauce",
    "i102": "Japanese karaage fried chicken pieces with a lemon wedge on a plate",
    "i103": "two Japanese soft-boiled marinated ramen eggs halved on a small plate",
    "i104": "a bowl of Japanese wakame seaweed salad",
    "i105": "a Japanese ramen set for two, two bowls of ramen with gyoza and karaage on a dark wooden table",
    # --- r008 首爾拌飯與炸雞 (Korean bibimbap / fried chicken) ---
    "i106": "Korean beef bibimbap in a sizzling hot stone bowl with vegetables, an egg and gochujang",
    "i108": "Korean pork kimchi stew bubbling in a stone pot",
    "i109": "Korean spicy tteokbokki rice cakes with melted cheese in a black bowl",
    "i110": "Korean chicken bibimbap in a stone bowl with vegetables and an egg",
    "i111": "Korean vegetarian mushroom bibimbap in a stone bowl with assorted vegetables and an egg",
    "i112": "Korean original crispy fried chicken pieces in a black bowl with pickled radish",
    "i113": "Korean honey garlic glazed fried chicken pieces in a black bowl, glossy",
    "i114": "Korean spicy yangnyeom sauce fried chicken pieces with sesame in a black bowl",
    "i115": "a paper cup of Korean boneless popcorn fried chicken",
    "i116": "a small dish of Korean napa cabbage kimchi",
    "i117": "Korean gimbap seaweed rice rolls sliced on a plate",
    "i118": "a bowl of Korean fish cake soup with broth",
    "i119": "a glass of Korean yuzu citron sparkling soda with ice",
    "i120": "a Korean fried chicken sharing combo, assorted fried chicken with tteokbokki and drinks on a dark table",
    # --- r009 美式街角漢堡 (American burgers) ---
    "i122": "an American spicy fried chicken burger with lettuce and mayo, on a wooden board with fries",
    "i123": "an American bacon peanut butter beef burger on a wooden board",
    "i124": "an American double beef cheeseburger with two patties on a wooden board",
    "i125": "an American mushroom swiss cheeseburger on a wooden board",
    "i126": "an American grilled chicken breast burger with avocado on a wooden board",
    "i127": "a vegetarian bean patty burger with lettuce and tomato on a wooden board",
    "i128": "a basket of American crispy french fries",
    "i129": "a basket of truffle parmesan cheese fries with herbs",
    "i130": "a plate of American buffalo chicken wings with celery and blue cheese dip",
    "i131": "a basket of American crispy onion rings",
    "i132": "a bowl of Caesar salad with croutons and parmesan",
    "i133": "a glass of cola with ice cubes",
    "i134": "a tall glass of vanilla milkshake with whipped cream and a straw",
    "i135": "an American burger combo for two, two cheeseburgers with fries and drinks on a wooden board",
    # --- r010 巷弄披薩義麵館 (Italian pizza / pasta) ---
    "i137": "Italian spaghetti carbonara with bacon and egg yolk, in a white bowl",
    "i138": "a Hawaiian pizza with ham and pineapple, on a wooden board",
    "i139": "a spicy beef pizza with jalapenos, on a wooden board",
    "i140": "a wild mushroom and truffle pizza, on a wooden board",
    "i141": "a seafood supreme pizza with shrimp, squid and mussels, on a wooden board",
    "i142": "a four cheese pizza, on a wooden board",
    "i143": "Italian spaghetti vongole with clams, garlic and chili, in a white bowl",
    "i144": "Italian spaghetti bolognese with tomato meat sauce, in a white bowl",
    "i145": "Italian penne pasta with basil pesto and grilled chicken, in a white bowl",
    "i146": "Italian pumpkin and wild mushroom risotto, in a white bowl",
    "i147": "Italian baked seafood macaroni gratin with melted cheese, in a baking dish",
    "i148": "an Italian salad with balsamic vinaigrette, in a bowl",
    "i149": "a slice of Italian tiramisu dusted with cocoa, on a plate",
    "i150": "an Italian pizza party set for four, multiple pizzas and pasta on a rustic wooden table",
}


def store_of(item_id: str) -> int:
    """i001..i015 -> 1, i016..i030 -> 2, ... i136..i150 -> 10."""
    n = int(item_id[1:])
    return (n - 1) // 15 + 1


def prompt_for(item_id: str) -> str:
    return (
        "professional food photography of "
        + DISHES[item_id]
        + STORE_STYLE[store_of(item_id)]
    )


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--size", default="1024x1024", help="Image size WxH (default 1024x1024).")
    p.add_argument("--only", default="", help="Comma-separated item ids to render (default: all).")
    p.add_argument("--force", action="store_true", help="Re-render even if the .jpg already exists.")
    p.add_argument("--include-reps", action="store_true", help="Also render the 10 store representative items.")
    p.add_argument("--quality", type=int, default=88, help="JPEG quality 1-100 (default 88).")
    return p.parse_args()


def main() -> int:
    args = parse_args()
    gen = os.path.join(BONSAI_DIR, "scripts", "generate.sh")
    if not os.path.isfile(gen):
        print(f"error: generate.sh not found at {gen}. Set BONSAI_DIR.", file=sys.stderr)
        return 1
    os.makedirs(ITEMS_DIR, exist_ok=True)

    if args.only:
        targets = [s.strip() for s in args.only.split(",") if s.strip()]
    else:
        targets = sorted(DISHES, key=lambda i: int(i[1:]))
        if args.include_reps:
            targets = sorted(set(targets) | REP_ITEMS, key=lambda i: int(i[1:]))

    total = len(targets)
    done = 0
    for item_id in targets:
        if item_id in REP_ITEMS and not args.include_reps and item_id not in (
            args.only.split(",") if args.only else []
        ):
            continue
        if item_id not in DISHES and item_id not in REP_ITEMS:
            print(f"skip {item_id}: no dish description", file=sys.stderr)
            continue
        out_jpg = os.path.join(ITEMS_DIR, f"{item_id}.jpg")
        if os.path.exists(out_jpg) and not args.force:
            print(f"[skip] {item_id} (exists)")
            continue
        done += 1
        prompt = prompt_for(item_id)
        # Deterministic per-item seed so re-runs reproduce the same photo.
        seed = 10000 + int(item_id[1:])
        print(f"[{done}/{total}] {item_id} seed={seed}")
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as tmp:
            tmp_png = tmp.name
        try:
            subprocess.run(
                [gen, "--size", args.size, "--seed", str(seed),
                 "--prompt", prompt, "--output", tmp_png],
                check=True, cwd=BONSAI_DIR,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
            subprocess.run(
                ["sips", "-s", "format", "jpeg",
                 "-s", "formatOptions", str(args.quality),
                 tmp_png, "--out", out_jpg],
                check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
        finally:
            if os.path.exists(tmp_png):
                os.remove(tmp_png)
    print(f"done: rendered {done} image(s) into {ITEMS_DIR}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
