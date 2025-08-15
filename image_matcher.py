#!/usr/bin/env python3
"""
画像マッチング用Pythonスクリプト
pyautoguiのlocateOnScreen機能を使用
"""

import sys
import pyautogui
import json
from pathlib import Path

def find_image_on_screen(target_image_path, confidence=0.8):
    """
    画面上で指定された画像を検索し、見つかった場合は中心座標を返す
    
    Args:
        target_image_path: 検索対象の画像ファイルパス
        confidence: マッチング信頼度 (0.0-1.0)
    
    Returns:
        dict: {"found": bool, "x": int, "y": int, "confidence": float}
    """
    try:
        # 画像ファイルの存在確認
        if not Path(target_image_path).exists():
            return {
                "found": False,
                "error": f"画像ファイルが見つかりません: {target_image_path}",
                "x": 0,
                "y": 0,
                "confidence": 0.0
            }
        
        print(f"🔍 pyautogui画像検索開始: {target_image_path} (信頼度: {confidence})", file=sys.stderr)
        
        # pyautoguiで画像を検索（OpenCVが無い場合はconfidenceパラメータを使わない）
        try:
            location = pyautogui.locateOnScreen(target_image_path, confidence=confidence)
        except (TypeError, Exception) as e:
            # OpenCVが無い場合はconfidenceパラメータなしで実行
            if "confidence" in str(e) or "OpenCV" in str(e):
                print("⚠️ OpenCVが見つからないため、confidenceパラメータなしで実行", file=sys.stderr)
                location = pyautogui.locateOnScreen(target_image_path)
            else:
                raise e
        
        if location is not None:
            # 中心座標を計算
            center_x, center_y = pyautogui.center(location)
            
            result = {
                "found": True,
                "x": int(center_x),
                "y": int(center_y),
                "confidence": float(confidence),
                "region": {
                    "left": location.left,
                    "top": location.top,
                    "width": location.width,
                    "height": location.height
                }
            }
            
            print(f"✅ 画像発見: 座標({center_x}, {center_y}) 領域{location}", file=sys.stderr)
            return result
        else:
            print(f"❌ 画像が見つかりませんでした: {target_image_path}", file=sys.stderr)
            return {
                "found": False,
                "x": 0,
                "y": 0,
                "confidence": 0.0
            }
            
    except Exception as e:
        print(f"❌ エラー発生: {str(e)}", file=sys.stderr)
        return {
            "found": False,
            "error": str(e),
            "x": 0,
            "y": 0,
            "confidence": 0.0
        }

def main():
    """メイン関数 - コマンドライン引数から画像パスと信頼度を取得"""
    if len(sys.argv) < 2:
        print("使用法: python image_matcher.py <画像パス> [信頼度]", file=sys.stderr)
        sys.exit(1)
    
    target_image = sys.argv[1]
    confidence = float(sys.argv[2]) if len(sys.argv) > 2 else 0.8
    
    # 結果を取得
    result = find_image_on_screen(target_image, confidence)
    
    # JSON形式で結果を出力（標準出力）
    print(json.dumps(result, ensure_ascii=False, indent=2))

if __name__ == "__main__":
    main()